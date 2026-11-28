// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/vektah/gqlparser/v2/gqlerror"

	gqlerrors "github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph/errors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph/model"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/kubeproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/envvars"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/iso8601"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// mapSubscription maps a latest-value source onto the returned channel until ctx ends or sub
// closes. No separate snapshot: the source is current-on-subscribe, so its first value IS
// the snapshot. unsub runs once on teardown, where out is also closed.
func mapSubscription[S, G any](
	ctx context.Context,
	sub <-chan S,
	unsub func(),
	mapFn func(S) G,
) <-chan G {
	out := make(chan G)
	go func() {
		defer close(out)
		defer unsub()

		for {
			select {
			case <-ctx.Done():
				return
			case s, ok := <-sub:
				if !ok {
					return
				}
				select {
				case out <- mapFn(s):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// ptrSlice maps a value slice onto pointers into it: the services hand out []T while
// gqlgen's bindings want []*T.
func ptrSlice[T any](items []T) []*T {
	out := make([]*T, len(items))
	for i := range items {
		out[i] = &items[i]
	}
	return out
}

// ptrSubscription is ptrSlice for streams, which nearly every cluster subscription resolver
// needs. The source is current-on-subscribe, so there's nothing to unsubscribe.
func ptrSubscription[T any](ctx context.Context, sub <-chan T) <-chan *T {
	return mapSubscription(ctx, sub, func() {}, func(v T) *T { return &v })
}

// nullTime unwraps the sql.NullTime a record holds so a watch can diff it with ==;
// the wire wants a pointer.
func nullTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

// chatRefusals maps the service's named errors onto the wire's coded ones. The
// composer branches on why a send was refused and a Go error's identity does not
// cross GraphQL, so the code is what carries it. Anything absent stays opaque —
// a store failure is not something a client can act on.
var chatRefusals = []struct {
	err  error
	wire *gqlerror.Error
}{
	{chat.ErrBadRequest, gqlerrors.ErrValidationError},
	{chat.ErrChatGone, gqlerrors.ErrRecordNotFound},
	{chat.ErrClusterGone, gqlerrors.ErrRecordNotFound},
	{chat.ErrTurnInFlight, gqlerrors.ErrConflict},
	{chat.ErrStopping, gqlerrors.ErrServiceUnavailable},
	// A chat the model cannot read whole is the model's limit, not the chat's
	// state: the composer names it so the user can pick another model.
	{chat.ErrChatContextFull, gqlerrors.ErrChatContextFull},
	// The sender saw the other switch, so the composer says it changed rather than
	// running the turn where the user did not look.
	{chat.ErrChatSandboxChanged, gqlerrors.ErrChatSandboxChanged},
	{chat.ErrGrantGone, gqlerrors.ErrRecordNotFound},
	{chat.ErrChatNetworkChanged, gqlerrors.ErrChatNetworkChanged},
}

// clusterRefusals maps the cluster service's named errors onto wire codes: an id
// naming nothing is not found; a record its source still declares, or one that
// will not be connected, disagrees with the record's own state, the same shape as a
// turn already in flight; a cluster that cannot be the default is a bad request.
var clusterRefusals = []struct {
	err  error
	wire *gqlerror.Error
}{
	{cluster.ErrNotFound, gqlerrors.ErrRecordNotFound},
	{cluster.ErrDeclaredBySource, gqlerrors.ErrConflict},
	{cluster.ErrNotConnectable, gqlerrors.ErrConflict},
	{cluster.ErrCannotBeDefault, gqlerrors.ErrValidationError},
}

// clusterErr is what every cluster mutation returns an error through.
func clusterErr(err error) error {
	for _, r := range clusterRefusals {
		if errors.Is(err, r.err) {
			return gqlerrors.Clone(r.wire)
		}
	}
	return err
}

// memoryRefusals maps the memory service's named errors onto wire codes. The
// dialog keeps the user's draft on each and says why.
var memoryRefusals = []struct {
	err  error
	wire *gqlerror.Error
}{
	{memory.ErrBadInput, gqlerrors.ErrValidationError},
	{memory.ErrNotFound, gqlerrors.ErrRecordNotFound},
	{memory.ErrClusterGone, gqlerrors.ErrRecordNotFound},
	{memory.ErrNameTaken, gqlerrors.ErrMemoryNameTaken},
	{memory.ErrFull, gqlerrors.ErrMemoryFull},
	{memory.ErrSecret, gqlerrors.ErrMemorySecret},
	{memory.ErrStopping, gqlerrors.ErrServiceUnavailable},
}

// memoryErr is what every memory resolver returns an error through.
func memoryErr(err error) error {
	for _, r := range memoryRefusals {
		if errors.Is(err, r.err) {
			return gqlerrors.Clone(r.wire)
		}
	}
	return err
}

// monitorRefusals maps the monitor's named errors onto wire codes: an unknown
// target or incident, a triage the incident cannot take, a triage model with no
// ids, and a subscription or a triage asked for as the monitor stops.
var monitorRefusals = []struct {
	err  error
	wire *gqlerror.Error
}{
	{monitor.ErrNoTarget, gqlerrors.ErrRecordNotFound},
	{monitor.ErrNoIncident, gqlerrors.ErrRecordNotFound},
	{monitor.ErrTriageQueued, gqlerrors.ErrValidationError},
	{monitor.ErrCannotTriage, gqlerrors.ErrValidationError},
	{monitor.ErrTargetMoved, gqlerrors.ErrValidationError},
	{monitor.ErrInvalidTriageModel, gqlerrors.ErrValidationError},
	{monitor.ErrStopping, gqlerrors.ErrServiceUnavailable},
}

// monitorErr is what every monitoring field returns an error through.
func monitorErr(err error) error {
	for _, r := range monitorRefusals {
		if errors.Is(err, r.err) {
			return gqlerrors.Clone(r.wire)
		}
	}
	return err
}

// monitorIntegrations is each registered integration's value on the wire, by name.
var monitorIntegrations = map[string]model.MonitorIntegration{
	kubesignals.Name: model.MonitorIntegrationKubernetes,
}

// monitorIntegrationOf is an integration's wire value; an error for one the enum
// lacks, which nulls the whole record that carries it, since the field is
// non-null. TestEveryMonitorIntegrationHasAValue pins the map to the enum.
func monitorIntegrationOf(name string) (model.MonitorIntegration, error) {
	if v, ok := monitorIntegrations[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("integration %q has no MonitorIntegration value", name)
}

// monitorAttributes is a stored JSON object of strings as label–value pairs,
// sorted by label.
func monitorAttributes(stored string) ([]*model.MonitorAttribute, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(stored), &m); err != nil {
		return nil, fmt.Errorf("read attributes: %w", err)
	}
	out := make([]*model.MonitorAttribute, 0, len(m))
	for _, label := range slices.Sorted(maps.Keys(m)) {
		out = append(out, &model.MonitorAttribute{Label: label, Value: m[label]})
	}
	return out, nil
}

// monitorUnknownSignals is a target's stored unknown signals, sorted by name.
func monitorUnknownSignals(stored string) ([]*model.MonitorUnknownSignal, error) {
	var m map[string]monitor.ReadErrorCode
	if err := json.Unmarshal([]byte(stored), &m); err != nil {
		return nil, fmt.Errorf("read unknown signals: %w", err)
	}
	out := make([]*model.MonitorUnknownSignal, 0, len(m))
	for _, name := range slices.Sorted(maps.Keys(m)) {
		out = append(out, &model.MonitorUnknownSignal{Name: name, Reason: m[name]})
	}
	return out, nil
}

// nonZero is &v, or nil for v's zero value: the wire's null for a record's unset field.
func nonZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// connectorErr is a refused Connect or Disconnect as the wire codes it: while
// Restoring, during an attempt, while connected, while a key the keyring
// would not delete is still there, or with no OAuth client.
func connectorErr(err error) error {
	for _, refusal := range []error{connectors.ErrRestoring, connectors.ErrSigningIn, connectors.ErrConnected, connectors.ErrNotDeleted, connectors.ErrNoClient} {
		if errors.Is(err, refusal) {
			return gqlerrors.Clone(gqlerrors.ErrValidationError)
		}
	}
	return err
}

// providerKeyErr is a refused Save or Delete as the wire codes it.
func providerKeyErr(err error) error {
	switch {
	case errors.Is(err, providercredentials.ErrUnknownProvider), errors.Is(err, providercredentials.ErrBadKey),
		errors.Is(err, providercredentials.ErrNoKey), errors.Is(err, providercredentials.ErrRestoring):
		return gqlerrors.Clone(gqlerrors.ErrValidationError)
	case errors.Is(err, providercredentials.ErrNotSaved):
		return gqlerrors.Clone(gqlerrors.ErrProviderKeyNotSaved)
	case errors.Is(err, providercredentials.ErrNotDeleted):
		return gqlerrors.Clone(gqlerrors.ErrProviderKeyNotDeleted)
	}
	return err
}

// modelsOf is every provider's models as the wire lists them, in picker order.
func modelsOf(providers []llm.Provider) []*model.Model {
	var out []*model.Model
	for _, p := range providers {
		provider := &model.Provider{ID: p.ID, Label: p.Label, LlmDialect: &p.Dialect}
		for _, m := range p.Catalog {
			out = append(out, &model.Model{
				Provider: provider, ID: m.ID, Label: m.Label, Efforts: m.Efforts, DefaultEffort: m.DefaultEffort,
			})
		}
	}
	return out
}

// chatErr is what every chat resolver returns an error through.
func chatErr(err error) error {
	for _, r := range chatRefusals {
		if errors.Is(err, r.err) {
			return gqlerrors.Clone(r.wire)
		}
	}
	return err
}

// logsErr is what both log resolvers return an error through: a refusal is a
// validation error whose rule is its code and whose message is its words.
func logsErr(err error) error {
	r, ok := refusal.As(err)
	if !ok {
		return err
	}
	return gqlerrors.NewValidationError(string(r.Code), r.Reason)
}

// fetchRequestOf is the wire's fetch request as the service takes it, the cursor
// passed through unread. A span it cannot read, or over the service's cap, is its
// one refusal; every other mismatch is the service's to refuse.
func fetchRequestOf(in model.LogsFetchInput) (logs.FetchRequest, error) {
	before, err := paddingOf(in.Before)
	if err != nil {
		return logs.FetchRequest{}, err
	}
	after, err := paddingOf(in.After)
	if err != nil {
		return logs.FetchRequest{}, err
	}
	return logs.FetchRequest{
		Scope:           valueOf(in.Scope),
		Window:          logs.Window{Since: in.Since, Until: in.Until, Before: before, After: after},
		Seen:            in.Seen,
		PageFrom:        valueOf(in.PageFrom),
		Cursor:          in.Cursor,
		PageSize:        valueOf(in.PageSize),
		MaxMessageBytes: valueOf(in.MaxMessageBytes),
		MaxPageBytes:    valueOf(in.MaxPageBytes),
	}, nil
}

// paddingOf is one side's padding as the service takes it, none for nil.
func paddingOf(in *model.LogsPaddingInput) (logs.Padding, error) {
	if in == nil {
		return logs.Padding{}, nil
	}
	p := logs.Padding{Lines: valueOf(in.Lines)}
	if in.Span != nil {
		var err error
		if p.Span, err = spanOf(*in.Span); err != nil {
			return logs.Padding{}, refusal.New(refusal.CodeBadPadding, err.Error())
		}
	}
	return p, nil
}

// spanOf is s read as a padding's span, under the service's cap.
func spanOf(s string) (time.Duration, error) {
	p, err := iso8601.Parse(s)
	if err != nil {
		return 0, err
	}
	return iso8601.Span(p, logs.MaxPaddingSpan)
}

// pageOf is the service's page on the wire.
func pageOf(resp logs.FetchResponse) *model.LogsFetchResult {
	return &model.LogsFetchResult{
		Request:    fetchRequestOut(resp.Request),
		ResolvedAt: resp.ResolvedAt,
		Page:       &model.LogRecordsPlace{Prev: resp.Page.Prev, Next: resp.Page.Next, Follow: resp.Page.Follow},
		Records:    ptrSlice(resp.Records),
		Notices:    noticesOf(resp.Notices),
	}
}

// fetchRequestOut is the request as it ran, as the page echoes it: each span
// formatted, and no pageFrom on a continuation.
func fetchRequestOut(q logs.FetchRequest) *model.LogsFetchRequest {
	out := &model.LogsFetchRequest{
		Sources: ptrSlice(q.Sources), Filters: ptrSlice(q.Filters), Grep: q.Grep,
		Since: q.Since, Until: q.Until, Before: paddingOut(q.Before), After: paddingOut(q.After),
		PageSize: q.PageSize, MaxMessageBytes: q.MaxMessageBytes, MaxPageBytes: q.MaxPageBytes,
	}
	if q.PageFrom != "" {
		out.PageFrom = &q.PageFrom
	}
	return out
}

// paddingOut is one side's padding as it ran, its span formatted; none is an
// empty span.
func paddingOut(p logs.Padding) *tools.LogsPadding {
	out := tools.LogsPaddingOf(p)
	return &out
}

// noticesOf is a page's or a tail's notices on the wire.
func noticesOf(notices []logs.Notice) []*tools.LogsNotice {
	out := make([]*tools.LogsNotice, len(notices))
	for i, n := range notices {
		out[i] = noticeOf(n)
	}
	return out
}

// noticeOf is one notice on the wire.
func noticeOf(n logs.Notice) *tools.LogsNotice {
	return &tools.LogsNotice{Code: string(n.Code), Message: n.Message}
}

// tailRequestOf is the wire's tail request as the service takes it, the
// cursor passed through unread.
func tailRequestOf(in model.LogsTailInput) logs.TailRequest {
	return logs.TailRequest{
		Scope: valueOf(in.Scope), Since: in.Since, Seen: in.Seen, Cursor: in.Cursor, MaxMessageBytes: valueOf(in.MaxMessageBytes),
	}
}

// frameOf is one tail frame on the wire, its type the field the service set.
func frameOf(f logs.TailFrame) *model.LogsTailFrame {
	switch {
	case f.Record != nil:
		return &model.LogsTailFrame{Type: model.LogsTailFrameTypeLine, Record: f.Record}
	case f.Notice != nil:
		return &model.LogsTailFrame{Type: model.LogsTailFrameTypeNotice, Notice: noticeOf(*f.Notice)}
	default:
		ended := &tools.LogsNotice{Code: string(f.Ended.Code), Message: f.Ended.Reason}
		return &model.LogsTailFrame{Type: model.LogsTailFrameTypeEnded, Ended: ended}
	}
}

// valueOf is *p, or the zero value for nil.
func valueOf[T any](p *T) T {
	var v T
	if p != nil {
		v = *p
	}
	return v
}

// cloudAccess is the build's cloud platforms, each with its switch.
func (r *Resolver) cloudAccess(ctx context.Context) ([]*model.CloudAccess, error) {
	sw, err := r.Settings.CloudPlatformSwitches(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.CloudAccess, 0, len(r.CloudTargets))
	for _, target := range r.CloudTargets {
		out = append(out, &model.CloudAccess{Target: target, On: sw.On(target)})
	}
	return out, nil
}

// sandboxPathErr is what every sandbox path resolver returns an error
// through: a refusal in the user's words is a validation error carrying them,
// and anything else stays opaque.
func sandboxPathErr(err error) error {
	if refusal, ok := errors.AsType[settings.PathRefusal](err); ok {
		return gqlerrors.NewValidationError("sandbox-path", refusal.Error())
	}
	return err
}

// sandboxPathOrigins is each origin by its wire spelling.
var sandboxPathOrigins = map[settings.PathOrigin]model.SandboxPathOrigin{
	settings.OriginShell: model.SandboxPathOriginShell,
	settings.OriginTyped: model.SandboxPathOriginTyped,
}

// sandboxPath is the PATH setting on the wire, with each of its folders.
func (r *Resolver) sandboxPath(ctx context.Context) (*model.SandboxPath, error) {
	setting, folders, err := r.Settings.PathFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := &model.SandboxPath{Value: setting.Value, Refusal: optionalString(setting.Refusal), Folders: []*model.SandboxPathFolder{}}
	for _, f := range folders {
		out.Folders = append(out.Folders, &model.SandboxPathFolder{
			Path: f.Path, Origin: sandboxPathOrigins[f.Origin], Target: optionalString(f.Target),
			Searched: f.Searched, Reason: optionalString(f.Reason), Addable: f.Addable, Shared: f.Shared,
		})
	}
	return out, nil
}

// optionalString is s, nil when empty.
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// sandboxPathAfter is a save's or a refresh's answer once its write has
// committed: the setting, else savedUnread.
func (r *Resolver) sandboxPathAfter(ctx context.Context, done string) (*model.SandboxPath, error) {
	path, err := r.sandboxPath(ctx)
	if err != nil {
		return nil, savedUnread(done, err)
	}
	return path, nil
}

// snapshotOf is the wire's snapshot as the chat service takes it; nil for none.
func snapshotOf(in *model.LogsViewSnapshotInput) *chat.Snapshot {
	if in == nil {
		return nil
	}
	selected := make([]chat.SelectedLine, len(in.Selected))
	for i, l := range in.Selected {
		selected[i] = chat.SelectedLine{
			Timestamp: l.Timestamp, Namespace: l.Namespace, Pod: l.Pod, Container: l.Container,
			Previous: l.Previous, Message: l.Message,
		}
	}
	return &chat.Snapshot{
		CallID: in.CallID, From: in.From, To: in.To, Grep: in.Grep, Lines: in.Lines,
		AtEnd: in.AtEnd, PinnedToEnd: in.PinnedToEnd, Paused: in.Paused,
		Selected: selected, SelectedCount: in.SelectedCount,
	}
}

// noSandboxExecutables refuses a probe or a register on a machine with no sandbox.
func noSandboxExecutables() error {
	return gqlerrors.NewValidationError("sandbox-executable", settings.ErrNoSandbox.Error())
}

// sandboxExecutableErr is what the executable resolvers return an error through: a
// refusal in the user's words is a validation error carrying them, and
// anything else stays opaque.
func sandboxExecutableErr(err error) error {
	if refusal, ok := errors.AsType[settings.ExecutableRefusal](err); ok {
		return gqlerrors.NewValidationError("sandbox-executable", refusal.Error())
	}
	return err
}

// executablesReport is the probe's report over the registered executables,
// read now.
func (r *Resolver) executablesReport(ctx context.Context) ([]*model.SandboxExecutable, error) {
	registered, err := r.Settings.Executables(ctx)
	if err != nil {
		return nil, err
	}
	return sandboxExecutablesOf(r.Executables.Report(registered)), nil
}

// executablesAfter is a register's or a remove's answer once its write has
// committed: the report, else savedUnread.
func (r *Resolver) executablesAfter(ctx context.Context, done string) ([]*model.SandboxExecutable, error) {
	report, err := r.executablesReport(ctx)
	if err != nil {
		return nil, savedUnread(done, err)
	}
	return report, nil
}

// sandboxConsoleErr is what the console's run resolver returns an error
// through: a command already running is a conflict, a refusal in the user's
// words is a validation error carrying them, and anything else stays opaque.
func sandboxConsoleErr(err error) error {
	if errors.Is(err, bash.ErrConsoleBusy) {
		return gqlerrors.NewError("KUBETAIL_CONFLICT", err.Error())
	}
	if _, ok := errors.AsType[*bash.ConsoleRefusal](err); ok || errors.Is(err, bash.ErrNoConsole) || errors.Is(err, bash.ErrConsoleClosed) {
		return gqlerrors.NewValidationError("sandbox-console", err.Error())
	}
	return err
}

// sandboxConsoleRunOf is the console's last command on the wire; nil for none.
func sandboxConsoleRunOf(run *bash.ConsoleRun) *model.SandboxConsoleRun {
	if run == nil {
		return nil
	}
	out := &model.SandboxConsoleRun{
		Number: run.Number, Command: run.Command, Network: run.Network, Running: run.Running,
		Output: run.Output, Discarded: run.Discarded, ExitCode: run.ExitCode,
		DurationMs: int(run.Duration.Milliseconds()),
	}
	if run.ClusterID != "" {
		out.ClusterID = &run.ClusterID
	}
	if run.Notice != "" {
		out.Notice = &run.Notice
	}
	return out
}

// envVarErr is what the variable resolvers return an error through: a refusal
// in the user's words is a validation error carrying them, a missing cluster is
// not found, and anything else stays opaque.
func envVarErr(err error) error {
	if refusal, ok := errors.AsType[envvars.Refusal](err); ok {
		return gqlerrors.NewValidationError("env-var", refusal.Error())
	}
	if errors.Is(err, envvars.ErrClusterGone) {
		return gqlerrors.Clone(gqlerrors.ErrRecordNotFound)
	}
	return err
}

// sandboxExecutablesOf is reports on the wire, in order.
func sandboxExecutablesOf(reports []bash.ExecutableReport) []*model.SandboxExecutable {
	out := make([]*model.SandboxExecutable, len(reports))
	for i, r := range reports {
		out[i] = &model.SandboxExecutable{
			Name: r.Name, Invocation: r.Invocation, Registered: r.Registered, Probed: r.Probed,
			Resolved: r.Resolved, Shim: r.Shim, Target: r.Target, Ok: r.OK, Version: r.Version,
			Error: r.Error,
		}
		if r.Refusal != "" {
			out[i].Refusal = &r.Refusal
		}
	}
	return out
}

// folderErr is what every folder grant resolver returns an error through: a
// folder that cannot be granted is a validation error naming the check that
// refused it, a link's carrying the path it leads to, so the webview can offer
// that one.
func folderErr(err error) error {
	if refusal, ok := errors.AsType[settings.FolderRefusal](err); ok {
		e := gqlerrors.NewValidationError(refusal.Rule, refusal.Reason)
		if refusal.Target != "" {
			e.Extensions["target"] = refusal.Target
		}
		return e
	}
	return chatErr(err)
}

// sandboxFoldersAfter is a folder grant mutation's answer once its write has
// committed: the folders, else savedUnread.
func (r *Resolver) sandboxFoldersAfter(ctx context.Context, chatID apimeta.ChatID, done string) (*model.SandboxFolders, error) {
	folders, err := r.sandboxFolders(ctx, chatID)
	if err != nil {
		return nil, savedUnread(done, err)
	}
	return folders, nil
}

// savedUnread is a mutation's answer when its write committed but what it
// answers could not be read after: KUBETAIL_SAVED_UNREAD, saying done, so no one
// reads it as a refusal and tries again. The reason is logged, not sent.
func savedUnread(done string, err error) error {
	slog.Warn("read after a write failed", "write", done, "err", err)
	e := gqlerrors.Clone(gqlerrors.ErrSavedUnread)
	e.Message = done + ", but Kubetail could not read the settings back. Reopen Settings to see them."
	return e
}

// approvalErr is what approvalDecide returns an error through: a rule an
// answer would write that Kubetail cannot apply is a validation error saying
// why, and anything else is a chat refusal.
func approvalErr(err error) error {
	if refusal, ok := errors.AsType[settings.RuleRefusal](err); ok {
		return gqlerrors.NewValidationError("decision", refusal.Error())
	}
	return chatErr(err)
}

// granteeOf is whose folder grant a grant or revoke of duration names: the
// chat's for Chat, which must name it, and with no chat an always one. An
// always grant is never narrowed to a chat, and a chat's never widened to
// every chat.
func granteeOf(chatID *apimeta.ChatID, duration model.GrantDuration) (apimeta.ChatID, error) {
	if duration != model.GrantDurationChat {
		return "", nil
	}
	if chatID == nil || *chatID == "" {
		return "", gqlerrors.NewValidationError("chatID", "a grant for a chat names the chat")
	}
	return *chatID, nil
}

// sandboxFolders is the folders granted, always and for chatID ("" for no
// chat), with what no grant opens, as Settings and the composer draw them.
func (r *Resolver) sandboxFolders(ctx context.Context, chatID apimeta.ChatID) (*model.SandboxFolders, error) {
	always, chat, err := r.ChatSvc.FolderGrants(ctx, chatID)
	if err != nil {
		return nil, err
	}
	out := &model.SandboxFolders{
		Always: sandboxFoldersOf(always), Chat: sandboxFoldersOf(chat),
		Never: []string{}, Closed: []string{}, Wide: []string{},
	}
	if r.SandboxState.Available {
		out.Never = append(out.Never, r.Settings.NeverReadable(ctx)...)
		out.Closed = append(out.Closed, r.Settings.ClosedFolders(ctx)...)
		out.Wide = append(out.Wide, r.Settings.WideFolders(ctx)...)
	}
	return out, nil
}

// sandboxFoldersOf is grants on the wire, in order.
func sandboxFoldersOf(grants []chat.FolderGrant) []*model.SandboxFolder {
	out := make([]*model.SandboxFolder, len(grants))
	for i, g := range grants {
		out[i] = &model.SandboxFolder{ID: g.ID, Path: g.Path, Write: g.Write}
		if g.Refusal != "" {
			out[i].Refusal = &g.Refusal
		}
	}
	return out
}

// permissionSettings is the permission modes and rules as Settings shows
// them: Kubernetes, then each cloud platform the build has, each with its
// default mode and its accounts. Kubernetes lists every cluster with a
// kube-context that is not awaiting deletion; a cloud platform, each account
// with a mode of its own.
func (r *Resolver) permissionSettings(ctx context.Context) (*model.PermissionSettings, error) {
	clusters, err := r.ClusterSvc.Clusters().List(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.Settings.Modes(ctx)
	if err != nil {
		return nil, err
	}
	stored := map[permissions.Target]map[string]permissions.Mode{}
	for _, row := range rows {
		if stored[row.Target] == nil {
			stored[row.Target] = map[string]permissions.Mode{}
		}
		stored[row.Target][row.Account] = row.Mode
	}
	targetModes := func(target permissions.Target) *model.PermissionTargetModes {
		def, ok := stored[target]["*"]
		if !ok {
			def = permissions.ModeAsk
		}
		return &model.PermissionTargetModes{Target: target, DefaultMode: def, Accounts: []*model.PermissionAccountMode{}}
	}
	account := func(t *model.PermissionTargetModes, id, name string, cluster *apimeta.ClusterID) {
		m := &model.PermissionAccountMode{Account: id, Name: name, ClusterID: cluster, Mode: t.DefaultMode, Source: settings.SourceDefault}
		if mode, ok := stored[t.Target][id]; ok {
			m.Mode, m.Source = mode, settings.SourceEntry
		}
		t.Accounts = append(t.Accounts, m)
	}
	kube := targetModes(permissions.Kubernetes)
	for _, c := range clusters {
		if c.KubeContext() != "" && c.DeletionRequestedAt == nil {
			account(kube, string(c.ID), c.KubeContext(), &c.ID)
		}
	}
	targets := []*model.PermissionTargetModes{kube}
	for _, target := range r.CloudTargets {
		t := targetModes(target)
		for _, id := range slices.Sorted(maps.Keys(stored[target])) {
			if id != "*" {
				account(t, id, id, nil)
			}
		}
		targets = append(targets, t)
	}
	rules, err := r.Settings.Rules(ctx)
	if err != nil {
		return nil, err
	}
	return &model.PermissionSettings{
		Targets: targets, Rules: rulePointers(rules), AlwaysAsks: kubeproxy.AlwaysAsks, AccessControl: kubeproxy.AccessControl,
	}, nil
}

// permissionsAfter is a permission mutation's answer: its refusal, else the
// settings it left.
func (r *Resolver) permissionsAfter(ctx context.Context, err error) (*model.PermissionSettings, error) {
	if refusal, ok := errors.AsType[settings.RuleRefusal](err); ok {
		return nil, gqlerrors.NewValidationError("permission", refusal.Error())
	}
	if refusal, ok := errors.AsType[settings.ModeRefusal](err); ok {
		return nil, gqlerrors.NewValidationError("permission", refusal.Error())
	}
	switch {
	case errors.Is(err, settings.ErrClusterGone):
		return nil, gqlerrors.Clone(gqlerrors.ErrRecordNotFound)
	case errors.Is(err, settings.ErrNoKubeContext), errors.Is(err, settings.ErrNoRule):
		return nil, gqlerrors.NewValidationError("permission", err.Error())
	case err != nil:
		return nil, err
	}
	out, err := r.permissionSettings(ctx)
	if err != nil {
		return nil, savedUnread("The change was saved", err)
	}
	return out, nil
}

// ruleLine is rule in the user's words. A Kubernetes rule naming a cluster
// names it by the record's kube-context, or by its id once the record has
// none or is gone; any other rule names its account by the account itself.
func (r *Resolver) ruleLine(ctx context.Context, rule permissions.Rule) (string, error) {
	account, literal := rule.Account()
	if rule.Target != string(permissions.Kubernetes) || !literal {
		return rule.Line(""), nil
	}
	c, err := r.ClusterSvc.Clusters().Get(ctx, apimeta.ClusterID(account))
	if err != nil {
		return "", err
	}
	if c != nil && c.KubeContext() != "" {
		account = c.KubeContext()
	}
	return rule.Line(account), nil
}

// ruleOf is a rule input as the settings write it: a null principal or
// target is every one.
func ruleOf(input model.PermissionRuleInput) permissions.Rule {
	r := permissions.Rule{
		Effect: input.Effect, AccessLevel: input.AccessLevel, Principal: "*", Target: "*",
		Scope: input.Scope, Service: input.Service, Operation: input.Operation, Resource: input.Resource,
	}
	if input.Principal != nil {
		r.Principal = string(*input.Principal)
	}
	if input.Target != nil {
		r.Target = string(*input.Target)
	}
	return r
}

// rulePointers is rules as gqlgen serves a list of them: never null, and one
// entry per id, since a row Kubetail cannot apply reads as two rules under its id.
func rulePointers(rules []permissions.Rule) []*permissions.Rule {
	out := make([]*permissions.Rule, 0, len(rules))
	for i := range rules {
		if i > 0 && rules[i].ID == rules[i-1].ID {
			continue
		}
		out = append(out, &rules[i])
	}
	return out
}

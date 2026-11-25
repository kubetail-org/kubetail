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

// Package clustercard renders what the model is told about the cluster a chat
// is under: a short, bounded card attached to the user's message. Render is a
// pure function of Facts; card.go fills them from the cluster service.
package clustercard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/fencejson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// The card is the Cluster section of a context block: a fenced JSON object on
// one line. The wire wraps the block in <context> tags; the record holds the
// sections alone. JSON is what keeps a value from forging the shape — every
// string is escaped, so nothing the server spells can open a section or end the
// fence.
const (
	cardHead   = "## Cluster\n\n```json\n"
	fenceClose = "\n```"
	// The seam between two sections, and the head of a section after its title.
	sectionEnd  = "\n```\n\n## "
	sectionBody = "\n\n```json\n"
)

// WithSection is card with a section appended: title as a heading, then raw as a
// fenced JSON object on one line, re-marshalled so its escaping is the card's
// own. card is a rendered block, or "" for none, which opens a new one. raw must
// be one valid JSON value.
func WithSection(card, title string, raw json.RawMessage) (string, error) {
	// UseNumber keeps a number's digits: a float64 would round a large integer
	// and refuse an exponent past its range.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("clustercard: more than one JSON value")
	}
	body := string(marshal(v))
	if card == "" {
		return "## " + title + sectionBody + body + fenceClose, nil
	}
	return strings.TrimSuffix(card, fenceClose) + sectionEnd + title + sectionBody + body + fenceClose, nil
}

// Unavailable is the card sent when none can be rendered. The system prompt
// reads it as withdrawing every earlier card.
const Unavailable = cardHead + `{"unavailable":true}` + fenceClose

// Budget is the card's ceiling in bytes. The four shares below sum to it, each
// measured as the marshalled bytes of its part.
const Budget = 4096

const (
	// headShare covers the wrapper and every single value; the value caps bound it.
	headShare        = 640
	notWatchingShare = 512
	namespacesShare  = 1472
	groupsShare      = 1472

	// groupMax bounds one group's entry: the catalog cuts inside a group before
	// it drops one, since the group is the orientation and its kinds the detail.
	groupMax = 192
	// nameMax caps the display name, contextMax the kube-context and valueMax
	// every other single value, as marshalled. A command names the context
	// whole, and one cut short names no context outside a sandboxed run, so its
	// cap is past the longest a real one runs: an EKS ARN is about 150 bytes.
	nameMax    = 64
	contextMax = 192
	valueMax   = 40
)

// The status a list carries. The system prompt defines each; exported because a
// tool's answer uses the same words.
const (
	StatusWatching      = "watching"
	StatusPaused        = "paused"
	StatusLastKnown     = "last-known"
	StatusUnknown       = "unknown"
	StatusSyncing       = "syncing"
	StatusNotServed     = "not-served"
	StatusNotDiscovered = "not-discovered"
)

// The status the apiGroups section carries, beyond the list statuses.
const (
	statusDiscovered  = "discovered"
	statusPartial     = "partial"
	statusDiscovering = "discovering"
)

// The connection's TLS posture, read off the kubeconfig entry.
const (
	tlsVerified   = "verified"
	tlsUnverified = "unverified"
	tlsNone       = "none"
)

// Facts is what the card is rendered from: the cluster's record, and its active
// cache when it has one.
type Facts struct {
	// Name is the display name the user set; empty when none is.
	Name string
	// Context is the kube-context's name.
	Context string
	// Version is the server version the last probe read; empty when none has.
	Version string
	// Connection is the reason on the record's connection verdict.
	Connection string
	// TLS is tlsVerified, tlsUnverified or tlsNone; empty when the record has
	// no kubeconfig entry.
	TLS string
	// Cache is the active cache, nil for a cluster never identified.
	Cache *CacheFacts
}

// CacheFacts is one reading of the active cache.
type CacheFacts struct {
	// Health is the cache-wide verdict, which Freshness reads.
	Health cluster.ClusterCacheHealth
	// Discovery is the sweep's own verdict.
	Discovery string
	// Kinds is the catalog, each with its sync verdict.
	Kinds []Kind
	// Namespaces is the names the cache holds for v1/namespaces.
	Namespaces []string
}

// Kind is one catalog row beside its sync row.
type Kind struct {
	APIVersion string
	Kind       string
	Resource   string
	// Scope is the catalog's: "Namespaced" or "Cluster".
	Scope string
	// Reason is the kind's own sync verdict; empty when its worker has said nothing.
	Reason string
}

// The card's JSON shape: one section per question the model asks. Every list
// is sorted and cut to its share.
type card struct {
	Cluster    clusterSection    `json:"cluster"`
	Connection connectionSection `json:"connection"`
	Freshness  FreshnessSection  `json:"freshness"`
	// Inventory is nil for a cluster with no cache: nothing was read.
	Inventory *inventorySection `json:"inventory,omitempty"`
}

// clusterSection says which cluster this is.
type clusterSection struct {
	Name       string `json:"name,omitempty"`
	Context    string `json:"context"`
	Kubernetes string `json:"kubernetes,omitempty"`
}

// connectionSection says whether the app can reach it.
type connectionSection struct {
	Status string `json:"status"`
	TLS    string `json:"tls,omitempty"`
}

// FreshnessSection says whether what follows is current: one verdict over the
// active cache. Since is when the app last saw the cluster live, carried under
// last-known alone; NotWatching names the kinds whose own reason is a partial
// verdict's. KubeQuery's answer carries it too, and there alone the kinds never
// listed are named, their rows missing: Syncing those whose list is running,
// Unknown those whose is not.
type FreshnessSection struct {
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	Since       string `json:"since,omitempty"`
	NotWatching *list  `json:"notWatching,omitempty"`
	Syncing     *list  `json:"syncing,omitempty"`
	Unknown     *list  `json:"unknown,omitempty"`
}

// inventorySection says what is in it, each list under its own status.
type inventorySection struct {
	Namespaces *list   `json:"namespaces,omitempty"`
	APIGroups  *groups `json:"apiGroups,omitempty"`
}

// list is names under a verdict. More counts what a cut dropped.
type list struct {
	Status string   `json:"status,omitempty"`
	Reason string   `json:"reason,omitempty"`
	Names  []string `json:"names,omitempty"`
	More   int      `json:"more,omitempty"`
}

type groups struct {
	Status string  `json:"status"`
	Reason string  `json:"reason,omitempty"`
	Groups []group `json:"groups,omitempty"`
	More   int     `json:"more,omitempty"`
}

type group struct {
	Name  string   `json:"name"`
	Kinds []string `json:"kinds"`
	More  int      `json:"more,omitempty"`
}

// Render is the card for f, never over Budget.
func Render(f Facts) string {
	return cardHead + string(marshal(build(f))) + fenceClose
}

func build(f Facts) card {
	c := card{
		Cluster:    clusterSection{Name: cut(f.Name, nameMax), Context: cut(f.Context, contextMax), Kubernetes: cut(f.Version, valueMax)},
		Connection: connectionSection{Status: cut(f.Connection, valueMax), TLS: cut(f.TLS, valueMax)},
		Freshness:  Freshness(nil),
	}
	if f.Cache == nil {
		return c
	}
	c.Freshness = Freshness(&f.Cache.Health)
	c.Inventory = &inventorySection{Namespaces: namespaces(f.Cache), APIGroups: apiGroups(f.Cache)}
	return c
}

// Freshness is the health verdict as the card states it, syncing for a cluster
// with no cache (nil). A cache whose rows are not Trusted is syncing until it has
// listed, else unknown. The cache-wide arms read the rollup's reason; past them
// the reason is only the first offender's, so whether every kind is behind is
// decided by count. A cluster with sync switched off reads Paused with no counts,
// which answers before any count is read.
func Freshness(h *cluster.ClusterCacheHealth) FreshnessSection {
	if h == nil {
		return FreshnessSection{Status: StatusSyncing}
	}
	if !h.Trusted() {
		if h.Reason == "" || h.Reason == cluster.ReasonConnecting {
			return FreshnessSection{Status: StatusSyncing}
		}
		return FreshnessSection{Status: StatusUnknown, Reason: h.Reason}
	}
	switch h.Reason {
	case "Watching":
		return FreshnessSection{Status: StatusWatching}
	case "Paused":
		return FreshnessSection{Status: StatusPaused}
	}
	// Every non-Watching kind counts, syncing ones too: with none Watching, no
	// watch can move the live stamp.
	allBehind := len(h.UnhealthyKindRefs) >= h.TotalKinds-h.PausedKinds
	if !allBehind {
		// Only the kinds whose own reason is the verdict's: under Stale or
		// SyncFailed, a kind still on its cold list is neither.
		var names []string
		for _, ref := range h.UnhealthyKindRefs {
			if ref.Reason == h.Reason {
				names = append(names, kindName(ref.KindRef))
			}
		}
		if len(names) == 0 {
			return FreshnessSection{Status: statusPartial}
		}
		return FreshnessSection{Status: statusPartial, NotWatching: list{}.withNames(sorted(names), notWatchingShare)}
	}
	// With every kind behind, no watch can move the live stamp, so the card's
	// text holds still — the dedupe's premise.
	fr := FreshnessSection{Status: StatusLastKnown, Reason: cut(h.Reason, valueMax)}
	if h.LastLiveAt != nil {
		fr.Since = h.LastLiveAt.UTC().Format(time.RFC3339)
	}
	return fr
}

// ConnectingFreshness is the verdict over a Connecting cache for a reader that
// knows which kinds have listed, in this session or an earlier one, and whether
// the cache answers at all. A kind behind that has listed answers from rows that
// may be out of date, unless its reason is RefusedCacheWide: rows of another
// cluster, or a store that failed, go under Unknown. One that never listed is
// named, its rows incomplete: under Syncing while it has said nothing or a list
// is running, else under Unknown, since no list is running and nothing arrives
// until one does (SyncFailed, NoConnection). With a kind Watching, only the
// kinds behind are named; with every kind behind, the whole answer is
// last-known. Nothing answers until some kind has rows: then the verdict is
// Freshness's, syncing.
func ConnectingFreshness(h cluster.ClusterCacheHealth, listed map[cluster.KindRef]bool) (FreshnessSection, bool) {
	var old, syncing, unknown []string
	for _, ref := range h.UnansweredKindRefs {
		if listed[ref] {
			old = append(old, kindName(ref))
		} else {
			syncing = append(syncing, kindName(ref))
		}
	}
	for _, ref := range h.UnhealthyKindRefs {
		switch {
		case cluster.RefusedCacheWide(ref.Reason):
			unknown = append(unknown, kindName(ref.KindRef))
		case listed[ref.KindRef]:
			old = append(old, kindName(ref.KindRef))
		case ref.Reason == "Syncing" || ref.Reason == "Resyncing":
			syncing = append(syncing, kindName(ref.KindRef))
		default:
			unknown = append(unknown, kindName(ref.KindRef))
		}
	}
	behind := len(h.UnansweredKindRefs) + len(h.UnhealthyKindRefs)
	someWatching := behind < h.TotalKinds-h.PausedKinds
	if len(old) == 0 && !someWatching {
		return Freshness(&h), false
	}
	fr := FreshnessSection{Status: StatusLastKnown, Reason: cluster.ReasonConnecting}
	if someWatching {
		fr.Status = statusPartial
		if len(old) > 0 {
			fr.NotWatching = list{}.withNames(sorted(old), notWatchingShare)
		}
	}
	if len(syncing) > 0 {
		fr.Syncing = list{}.withNames(sorted(syncing), notWatchingShare)
	}
	if len(unknown) > 0 {
		fr.Unknown = list{}.withNames(sorted(unknown), notWatchingShare)
	}
	return fr, true
}

func kindName(ref cluster.KindRef) string { return ref.APIVersion + "/" + ref.Resource }

// MarshalFreshness is the section as the card marshals it, for an answer that
// carries it.
func MarshalFreshness(f FreshnessSection) json.RawMessage { return marshal(f) }

func namespaces(cf *CacheFacts) *list {
	names := sorted(cf.Namespaces)
	l, listed := verdict(cf, cf.FindKind("v1", "namespaces"), len(names) > 0)
	if !listed {
		return &l
	}
	return l.withNames(names, namespacesShare)
}

// verdict is Verdict as a list.
func verdict(cf *CacheFacts, k *Kind, hasRows bool) (list, bool) {
	status, reason, listed := Verdict(cf, k, hasRows)
	return list{Status: status, Reason: cut(reason, valueMax)}, listed
}

// Verdict is the status a kind's list carries, its reason, and whether its rows
// go under it — the one rule the card and a tool's answer share. k nil is a kind
// the catalog lacks, told apart by the discovery verdict. hasRows tells
// last-known from unknown. The cache-wide arms come before the row's own: a
// cache at its size limit, paused, or with a failed store arms no kind, so its
// rows alone would read as still syncing.
func Verdict(cf *CacheFacts, k *Kind, hasRows bool) (status, reason string, listed bool) {
	if k == nil {
		if cf.Discovery == "Discovered" {
			return StatusNotServed, "", false
		}
		return StatusNotDiscovered, "", false
	}
	switch cf.Health.Reason {
	case "SizeLimit", "StoreFailed":
		return StatusUnknown, cf.Health.Reason, false
	case "Paused":
		return StatusPaused, "", true
	}
	switch {
	case k.Reason == "Watching":
		return StatusWatching, "", true
	case k.Reason == "Paused":
		return StatusPaused, "", true
	case k.Reason == "IdentityMismatch":
		// The rows are another cluster's.
		return StatusUnknown, k.Reason, false
	case k.Reason == "":
		return StatusSyncing, "", false
	case hasRows:
		return StatusLastKnown, k.Reason, true
	default:
		return StatusUnknown, k.Reason, false
	}
}

// apiGroups is the catalog by API group, under the sweep's verdict. Only
// Discovered reads as currently served; a reason not named here is last-known,
// so a new verdict can never promote the catalog by omission.
func apiGroups(cf *CacheFacts) *groups {
	g := &groups{}
	switch cf.Discovery {
	case "IdentityMismatch":
		// The rows are another cluster's.
		return nil
	case "Discovered":
		g.Status = statusDiscovered
	case "Partial":
		g.Status = statusPartial
	case "Discovering", "":
		g.Status = statusDiscovering
		return g
	default:
		g.Status, g.Reason = StatusLastKnown, cut(cf.Discovery, valueMax)
	}
	all := groupEntries(cf.Kinds)
	n := Fit(len(all), groupsShare, func(k int) int {
		return size(groups{Status: g.Status, Reason: g.Reason, Groups: all[:k], More: len(all) - k})
	})
	g.Groups, g.More = all[:n], len(all)-n
	return g
}

// groupEntries is the catalog grouped by API group, sorted, each entry inside
// groupMax.
func groupEntries(kinds []Kind) []group {
	byGroup := map[string][]string{}
	for _, k := range kinds {
		name := groupOf(k.APIVersion)
		if !slices.Contains(byGroup[name], k.Kind) {
			byGroup[name] = append(byGroup[name], k.Kind)
		}
	}
	var all []group
	for _, name := range slices.Sorted(maps.Keys(byGroup)) {
		all = append(all, groupEntry(name, sorted(byGroup[name])))
	}
	return all
}

// groupEntry is one group inside groupMax: the name gets up to half, the kinds
// what is left.
func groupEntry(name string, kinds []string) group {
	g := group{Name: cut(name, groupMax/2)}
	n := Fit(len(kinds), groupMax, func(k int) int {
		return size(group{Name: g.Name, Kinds: kinds[:k], More: len(kinds) - k})
	})
	g.Kinds, g.More = kinds[:n], len(kinds)-n
	return g
}

// groupOf is the API group an apiVersion names; the core group is "core".
func groupOf(apiVersion string) string {
	if g, _, ok := strings.Cut(apiVersion, "/"); ok {
		return g
	}
	return "core"
}

// withNames is l holding the longest prefix of names that keeps it inside
// budget, and the count it dropped as More.
func (l list) withNames(names []string, budget int) *list {
	n := Fit(len(names), budget, func(k int) int {
		return size(list{Status: l.Status, Reason: l.Reason, Names: names[:k], More: len(names) - k})
	})
	l.Names, l.More = names[:n], len(names)-n
	return &l
}

// Fit is how many of n items to keep: the longest prefix whose size fits
// budget. An item costs more than the digit its marker loses, so the walk can
// stop at the first that overflows. The card's cut, and a tool's.
func Fit(n, budget int, size func(k int) int) int {
	k := 0
	for k < n && size(k+1) <= budget {
		k++
	}
	return k
}

// cut bounds s to limit bytes as marshalled — quotes and escapes included, since
// an escape can be six bytes for one — closing a cut with an ellipsis. The cut
// lands on a rune boundary.
func cut(s string, limit int) string {
	if size(s) <= limit {
		return s
	}
	const ellipsis = "…"
	keep := 0
	for i := range s {
		// i is a rune start. A prefix longer than limit cannot marshal under it.
		if i > limit || size(s[:i]+ellipsis) > limit {
			break
		}
		keep = i
	}
	return s[:keep] + ellipsis
}

// size is what v costs on the wire.
func size(v any) int { return len(marshal(v)) }

func marshal(v any) []byte {
	raw, err := fencejson.Marshal(v)
	if err != nil { // coverage:ignore strings and ints always marshal
		panic(err)
	}
	return raw
}

func sorted(items []string) []string {
	out := slices.Clone(items)
	slices.Sort(out)
	return out
}

// FindKind is the catalog row for one kind, nil when absent.
func (c *CacheFacts) FindKind(apiVersion, resource string) *Kind {
	for i := range c.Kinds {
		if c.Kinds[i].APIVersion == apiVersion && c.Kinds[i].Resource == resource {
			return &c.Kinds[i]
		}
	}
	return nil
}

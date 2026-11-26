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

package bash

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/azureproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// AzureLogin is the user's Azure login as a run reads it: services/azureauth.
type AzureLogin interface {
	Account(ctx context.Context) (azureproxy.Account, bool)
	azureproxy.Tokens
}

// azurePlatform is Azure's CloudPlatform: az in a run reaches Resource
// Manager over the door's TLS route, as a managed identity whose token is the
// grant's.
type azurePlatform struct {
	login AzureLogin
	// transport is shared by every run's proxy, so a run reuses the
	// connections to Resource Manager the last one opened.
	transport http.RoundTripper
	// wait bounds how long a mount waits for the login, so a run that
	// never touches az does not wait on a slow one; azureMountWait.
	wait time.Duration
}

// azureMountWait is how long a mount waits for the login's discovery, which
// runs on for the next run once a mount stops waiting.
const azureMountWait = 10 * time.Second

// NewAzure is Azure's cloud platform over login, reaching Resource Manager
// through transport.
func NewAzure(login AzureLogin, transport http.RoundTripper) CloudPlatform {
	return &azurePlatform{login: login, transport: transport, wait: azureMountWait}
}

func (p *azurePlatform) Target() permissions.Target { return permissions.Azure }

// Mount builds the run's grant and its TLS route at the loopback authority.
// With an account it writes az's configuration under the run's TMPDIR and
// sets its variables; with none, az in the run finds no login and the route
// answers the sign-in sentence.
func (p *azurePlatform) Mount(ctx context.Context, run CloudRun) (CloudMount, error) {
	door := "127.0.0.1:" + strconv.Itoa(run.Port)
	g := runproxy.NewGrant(permissions.Azure, run.On, run.Session, run.Asker, run.Refusal, cloudLimits)
	cfg := azureproxy.Config{Grant: g, Port: run.Port, Tokens: p.login, Transport: p.transport}
	m := CloudMount{Grant: g}
	waitCtx, cancel := context.WithTimeout(ctx, p.wait)
	acct, ok := p.login.Account(waitCtx)
	cancel()
	if ok {
		cfg.Account = &acct
		dir := filepath.Join(run.TmpDir, "azure")
		if err := writeAzureConfig(dir, acct, run.Port); err != nil {
			g.End()
			return CloudMount{}, err
		}
		m.Env = []string{
			"AZURE_CONFIG_DIR=" + dir,
			"IDENTITY_ENDPOINT=https://" + door + azureproxy.IdentityPath,
			"IDENTITY_HEADER=" + g.Token(),
			"REQUESTS_CA_BUNDLE=" + run.CAFile,
		}
	}
	m.Routes = []Route{{Host: door, TLS: true, Handler: azureproxy.New(cfg)}}
	return m, nil
}

// azureCloud is the name of the cloud a run's az is configured for: the
// user's, with Resource Manager at the door.
const azureCloud = "kubetail"

// writeAzureConfig writes az's three files into dir, each owner-only and
// never over a file. None holds a token, and only azureProfile.json, written
// by the encoder, holds the user's text.
func writeAzureConfig(dir string, acct azureproxy.Account, port int) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	profile, err := azureProfile(acct)
	if err != nil { // coverage:ignore strings, bools and slices always marshal
		return err
	}
	for name, content := range map[string][]byte{
		"clouds.config":     cloudsConfig(acct.Cloud, port),
		"config":            []byte("[cloud]\nname = " + azureCloud + "\n\n[core]\ncollect_telemetry = false\n"),
		"azureProfile.json": profile,
	} {
		if err := writeNew(filepath.Join(dir, name), content); err != nil { // coverage:ignore dir was made, empty, just above: only a full disk fails a write
			return err
		}
	}
	return nil
}

// iniKey and iniValue are what clouds.config may hold: az reads it with
// Python's configparser, which treats % as interpolation.
var (
	iniKey   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	iniValue = regexp.MustCompile(`^[!-$&-~]+$`)
)

// cloudsConfig is the user's cloud as az cloud register writes one, named
// kubetail, with Resource Manager at the door. An entry az cannot read safely
// is left out.
func cloudsConfig(c azureproxy.Cloud, port int) []byte {
	lines := []string{"endpoint_resource_manager = https://127.0.0.1:" + strconv.Itoa(port) + "/"}
	for prefix, table := range map[string]map[string]string{"endpoint_": c.Endpoints, "suffix_": c.Suffixes} {
		for k, v := range table {
			if k == "resourceManager" || !iniKey.MatchString(k) || !iniValue.MatchString(v) {
				continue
			}
			lines = append(lines, prefix+snake(k)+" = "+v)
		}
	}
	slices.Sort(lines)
	return []byte("[" + azureCloud + "]\n" + strings.Join(lines, "\n") + "\n")
}

// snake is az's attribute name for a key az cloud show writes in camel case:
// activeDirectoryResourceId is active_directory_resource_id.
func snake(camel string) string {
	var b strings.Builder
	for i, r := range camel {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// azureProfile is the user's subscriptions, each signed in as a managed
// identity, which makes az ask the run's identity endpoint for its token.
func azureProfile(acct azureproxy.Account) ([]byte, error) {
	type user struct {
		Name                 string `json:"name"`
		Type                 string `json:"type"`
		AssignedIdentityInfo string `json:"assignedIdentityInfo"`
	}
	type subscription struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		TenantID        string `json:"tenantId"`
		State           string `json:"state"`
		IsDefault       bool   `json:"isDefault"`
		EnvironmentName string `json:"environmentName"`
		User            user   `json:"user"`
	}
	var subs []subscription
	// The run's az needs a default; with the user's left out by discovery,
	// the first stands in.
	hasDefault := slices.ContainsFunc(acct.Subscriptions, func(s azureproxy.Subscription) bool { return s.IsDefault })
	for i, s := range acct.Subscriptions {
		subs = append(subs, subscription{
			ID: s.ID, Name: s.Name, TenantID: s.TenantID, State: "Enabled", IsDefault: s.IsDefault || !hasDefault && i == 0,
			EnvironmentName: azureCloud,
			User:            user{Name: "systemAssignedIdentity", Type: "servicePrincipal", AssignedIdentityInfo: "MSI"},
		})
	}
	return json.Marshal(map[string][]subscription{"subscriptions": subs})
}

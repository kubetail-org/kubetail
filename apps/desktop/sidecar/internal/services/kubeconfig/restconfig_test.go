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

package kubeconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// loadedConfig writes a kubeconfig holding contexts and returns it as the service
// loads it, which is the only form resolution may be handed: loading is what turns a
// relative certificate path into one that can be opened.
func loadedConfig(t *testing.T, path string, contexts ...string) *api.Config {
	t.Helper()
	writeKubeconfig(t, path, contexts...)

	svc := newServiceAt(t, path, testInterval)
	start(t, svc)

	cfg, read := svc.Get()
	require.True(t, read)
	return cfg
}

func TestResolveReturnsCredentialsForAContext(t *testing.T) {
	cfg := loadedConfig(t, filepath.Join(t.TempDir(), "config"), "prod")

	restCfg, err := resolve(cfg, "prod")
	require.NoError(t, err)

	assert.Equal(t, "https://prod.invalid", restCfg.Host)
}

// A kubeconfig may name its CA relative to itself, and only the loading rules resolve
// it. Resolving from a config assembled any other way leaves a path that cannot be
// opened from the process's working directory.
func TestResolveKeepsARelativeCertificateAuthorityUsable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("-----BEGIN CERTIFICATE-----\n"), 0o600))

	cfg := api.NewConfig()
	cfg.Contexts["prod"] = &api.Context{Cluster: "prod-cluster", AuthInfo: "prod-user"}
	cfg.Clusters["prod-cluster"] = &api.Cluster{
		Server:               "https://prod.invalid",
		CertificateAuthority: "ca.crt",
	}
	cfg.AuthInfos["prod-user"] = &api.AuthInfo{}
	require.NoError(t, clientcmd.WriteToFile(*cfg, path))

	svc := newServiceAt(t, path, testInterval)
	start(t, svc)
	loaded, read := svc.Get()
	require.True(t, read)

	restCfg, err := resolve(loaded, "prod")
	require.NoError(t, err)

	assert.FileExists(t, restCfg.TLSClientConfig.CAFile)
}

// --- RESTConfig ---

func TestRESTConfigResolvesAContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeKubeconfig(t, path, "prod")
	svc := newServiceAt(t, path, testInterval)
	start(t, svc)

	restCfg, key, err := svc.RESTConfig("prod")
	require.NoError(t, err)

	assert.Equal(t, "https://prod.invalid", restCfg.Host)
	assert.NotEmpty(t, key)
}

// Before the first read every context looks absent, and a caller told "not found"
// would record a live cluster as gone. It has to be able to tell the two apart.
func TestRESTConfigReportsThatNothingHasBeenReadYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeKubeconfig(t, path, "prod")

	_, _, err := newServiceAt(t, path, testInterval).RESTConfig("prod")

	assert.ErrorIs(t, err, ErrNotRead)
	assert.NotErrorIs(t, err, ErrContextNotFound)
}

// The key identifies credentials, not records: two contexts aimed at one cluster with
// one set of credentials must share the connection the pool caches under it.
func TestRESTConfigKeyIgnoresTheContextName(t *testing.T) {
	cfg := api.NewConfig()
	cfg.Clusters["prod-cluster"] = &api.Cluster{Server: "https://prod.invalid"}
	cfg.AuthInfos["prod-user"] = &api.AuthInfo{Token: "t"}
	for _, name := range []string{"prod", "prod-copy"} {
		cfg.Contexts[name] = &api.Context{Cluster: "prod-cluster", AuthInfo: "prod-user"}
	}

	_, first, err := restConfigFrom(cfg, "prod")
	require.NoError(t, err)
	_, second, err := restConfigFrom(cfg, "prod-copy")
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

// The key must describe the credentials it accompanies, so both come from one
// snapshot — a reload between two reads would key one context's proxy onto another's.
func TestRESTConfigKeyFollowsTheProxyURL(t *testing.T) {
	cfg := api.NewConfig()
	cfg.Contexts["prod"] = &api.Context{Cluster: "prod-cluster", AuthInfo: "prod-user"}
	cfg.Clusters["prod-cluster"] = &api.Cluster{Server: "https://prod.invalid"}
	cfg.AuthInfos["prod-user"] = &api.AuthInfo{Token: "t"}

	_, direct, err := restConfigFrom(cfg, "prod")
	require.NoError(t, err)

	cfg.Clusters["prod-cluster"].ProxyURL = "http://proxy.invalid"
	_, proxied, err := restConfigFrom(cfg, "prod")
	require.NoError(t, err)

	assert.NotEqual(t, direct, proxied)
}

// execKubeconfig points two contexts at one server through one exec plugin, differing
// only in the per-cluster exec extension — the shape the field exists for, where a
// user entry is shared across clusters and an audience is what separates them.
const execKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: prod-cluster
  cluster:
    server: https://shared.invalid
    extensions:
    - name: client.authentication.k8s.io/exec
      extension:
        audience: prod
- name: staging-cluster
  cluster:
    server: https://shared.invalid
    extensions:
    - name: client.authentication.k8s.io/exec
      extension:
        audience: staging
users:
- name: shared
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: get-token
      interactiveMode: Never
contexts:
- name: prod
  context:
    cluster: prod-cluster
    user: shared
- name: staging
  context:
    cluster: staging-cluster
    user: shared
`

// The plugin mints a different credential per audience, so the two contexts must not
// share a pooled connection even though every other field matches.
func TestRESTConfigKeySplitsOnThePerClusterExecConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(execKubeconfig), 0o600))
	svc := newServiceAt(t, path, testInterval)
	start(t, svc)

	_, prod, err := svc.RESTConfig("prod")
	require.NoError(t, err)
	_, staging, err := svc.RESTConfig("staging")
	require.NoError(t, err)

	assert.NotEqual(t, prod, staging)
}

// clientcmd's merge takes the user's *api.ExecConfig by pointer and then writes the
// cluster's exec extension through it, so resolving against the service's snapshot
// edits what every other reader — and the poll loop's unchanged check — sees. A user
// entry shared by two clusters is where it surfaces: the second resolve would leave
// the first one's credentials describing the other cluster's audience.
func TestResolveLeavesTheSnapshotAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(execKubeconfig), 0o600))
	svc := newServiceAt(t, path, testInterval)
	start(t, svc)

	cfg, read := svc.Get()
	require.True(t, read)
	before := cfg.DeepCopy()

	prod, err := resolve(cfg, "prod")
	require.NoError(t, err)
	_, err = resolve(cfg, "staging")
	require.NoError(t, err)

	assert.Equal(t, before, cfg, "the snapshot every other reader shares")

	ext, ok := prod.ExecProvider.Config.(*runtime.Unknown)
	require.True(t, ok)
	assert.Contains(t, string(ext.Raw), "prod", "prod's credentials must not pick up staging's audience")
}

// --- fingerprint ---

func TestFingerprintIsStableForUnchangedCredentials(t *testing.T) {
	cfg := &rest.Config{Host: "https://prod.invalid", BearerToken: "t"}

	assert.Equal(t, fingerprint(cfg, ""), fingerprint(cfg, ""))
}

// Every field is a reason to redial: the key is what the connection pool caches on, so
// a change that leaves it equal serves the old connection forever. Credentials and
// transport settings alike — both are things a pooled connection is built with.
func TestFingerprintChangesWithTheResolvedConfig(t *testing.T) {
	base := &rest.Config{Host: "https://prod.invalid", BearerToken: "t"}
	want := fingerprint(base, "")

	changed := map[string]*rest.Config{
		"host":     {Host: "https://staging.invalid", BearerToken: "t"},
		"token":    {Host: "https://prod.invalid", BearerToken: "u"},
		"username": {Host: "https://prod.invalid", BearerToken: "t", Username: "alice"},
		"compression": {
			Host:               "https://prod.invalid",
			BearerToken:        "t",
			DisableCompression: true,
		},
		"CA data": {
			Host:            "https://prod.invalid",
			BearerToken:     "t",
			TLSClientConfig: rest.TLSClientConfig{CAData: []byte("ca")},
		},
		"impersonation": {
			Host:        "https://prod.invalid",
			BearerToken: "t",
			Impersonate: rest.ImpersonationConfig{UserName: "alice"},
		},
		"exec plugin": {
			Host:         "https://prod.invalid",
			BearerToken:  "t",
			ExecProvider: &api.ExecConfig{Command: "get-token"},
		},
	}
	for what, cfg := range changed {
		t.Run(what, func(t *testing.T) {
			assert.NotEqual(t, want, fingerprint(cfg, ""))
		})
	}
}

// Every optional and variable-length section has to be delimited in the hash. Left as
// a bare run of values, two configs that authenticate completely differently produce
// the same run — and the pool then serves one a transport built for the other.
func TestFingerprintSeparatesAuthenticationSections(t *testing.T) {
	host := "https://prod.invalid"
	collisions := []struct {
		what string
		a, b *rest.Config
	}{{
		what: "an auth provider against an exec plugin",
		a: &rest.Config{Host: host, AuthProvider: &api.AuthProviderConfig{
			Name:   "x",
			Config: map[string]string{"a": "b"},
		}},
		b: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{
			Command:    "x",
			APIVersion: "a",
			Args:       []string{"b"},
		}},
	}, {
		what: "impersonated groups against an extra key and its values",
		a: &rest.Config{Host: host, Impersonate: rest.ImpersonationConfig{
			Groups: []string{"a", "b"},
		}},
		b: &rest.Config{Host: host, Impersonate: rest.ImpersonationConfig{
			Extra: map[string][]string{"a": {"b"}},
		}},
	}, {
		what: "an exec argument against an environment name",
		a: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{
			Command: "x",
			Args:    []string{"a"},
		}},
		b: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{
			Command: "x",
			Env:     []api.ExecEnvVar{{Name: "a"}},
		}},
	}, {
		what: "an empty exec plugin against none at all",
		a:    &rest.Config{Host: host, ExecProvider: &api.ExecConfig{}},
		b:    &rest.Config{Host: host},
	}, {
		// What the plugin is handed decides what it returns.
		what: "an exec plugin told about its cluster against one that is not",
		a: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{
			Command:            "x",
			ProvideClusterInfo: true,
		}},
		b: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{Command: "x"}},
	}, {
		what: "an exec plugin that may prompt against one that may not",
		a: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{
			Command:         "x",
			InteractiveMode: api.NeverExecInteractiveMode,
		}},
		b: &rest.Config{Host: host, ExecProvider: &api.ExecConfig{
			Command:         "x",
			InteractiveMode: api.IfAvailableExecInteractiveMode,
		}},
	}}

	for _, c := range collisions {
		t.Run(c.what, func(t *testing.T) {
			assert.NotEqual(t, fingerprint(c.a, ""), fingerprint(c.b, ""))
		})
	}
}

// The proxy is compiled into an opaque func by the time it reaches rest.Config, so it
// rides alongside — two contexts differing only by proxy reach different endpoints.
func TestFingerprintCoversTheProxyURL(t *testing.T) {
	cfg := &rest.Config{Host: "https://prod.invalid"}

	assert.NotEqual(t, fingerprint(cfg, ""), fingerprint(cfg, "http://proxy.invalid"))
}

// stubExecConfig is an exec config that is not the *runtime.Unknown a kubeconfig
// decodes to — what an in-process caller could hand us. unmarshalable makes encoding
// it fail, which is the last resort the hash still has to produce something for.
type stubExecConfig struct {
	runtime.Object
	Audience      string
	unmarshalable chan struct{}
}

func (s stubExecConfig) MarshalJSON() ([]byte, error) {
	if s.unmarshalable != nil {
		return nil, errors.New("cannot encode")
	}
	return json.Marshal(s.Audience)
}

// An exec config this does not recognize is still credential input, so it has to reach
// the hash rather than being dropped — dropping it makes two clusters that differ only
// there share a connection.
func TestFingerprintCoversAnUnrecognizedExecConfig(t *testing.T) {
	withConfig := func(cfg runtime.Object) *rest.Config {
		return &rest.Config{
			Host:         "https://prod.invalid",
			ExecProvider: &api.ExecConfig{Command: "get-token", Config: cfg},
		}
	}

	none := fingerprint(withConfig(nil), "")
	prod := fingerprint(withConfig(stubExecConfig{Audience: "prod"}), "")
	staging := fingerprint(withConfig(stubExecConfig{Audience: "staging"}), "")

	assert.NotEqual(t, none, prod)
	assert.NotEqual(t, prod, staging)
}

// A config that cannot be encoded must still hash as itself. Returning nothing would
// make every unencodable config identical, which is the collision this guards.
func TestFingerprintCoversAnUnencodableExecConfig(t *testing.T) {
	withConfig := func(audience string) *rest.Config {
		return &rest.Config{
			Host: "https://prod.invalid",
			ExecProvider: &api.ExecConfig{Command: "get-token", Config: stubExecConfig{
				Audience:      audience,
				unmarshalable: make(chan struct{}),
			}},
		}
	}

	assert.NotEqual(t, fingerprint(withConfig("prod"), ""), fingerprint(withConfig("staging"), ""))
}

func TestContextProxyURL(t *testing.T) {
	cfg := api.NewConfig()
	cfg.Contexts["prod"] = &api.Context{Cluster: "prod-cluster"}
	cfg.Clusters["prod-cluster"] = &api.Cluster{ProxyURL: "http://proxy.invalid"}
	cfg.Contexts["bare"] = &api.Context{Cluster: "missing"}

	assert.Equal(t, "http://proxy.invalid", contextProxyURL(cfg, "prod"))
	assert.Empty(t, contextProxyURL(cfg, "bare"), "a context naming no known cluster")
	assert.Empty(t, contextProxyURL(cfg, "absent"), "a context that is not there")
}

// A context the kubeconfig no longer holds is the ordinary orphaned-record case, and
// the caller acts on it rather than logging it — so it is a sentinel, not prose.
func TestResolveReportsAMissingContext(t *testing.T) {
	cfg := loadedConfig(t, filepath.Join(t.TempDir(), "config"), "prod")

	_, err := resolve(cfg, "staging")
	assert.ErrorIs(t, err, ErrContextNotFound)
}

// A context can name a cluster the kubeconfig does not hold — half-hand-edited, or a
// merge that dropped one. clientcmd rejects it, and that has to reach the caller
// rather than a config with no server in it.
func TestResolveReportsAnUnusableContext(t *testing.T) {
	cfg := api.NewConfig()
	cfg.Contexts["prod"] = &api.Context{Cluster: "absent-cluster", AuthInfo: "prod-user"}
	cfg.AuthInfos["prod-user"] = &api.AuthInfo{}

	_, err := resolve(cfg, "prod")

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrContextNotFound, "the context is there; what it points at is not")
}

// RESTConfig has read, so a bad context name is the resolver's error to pass through
// rather than something it reports as unread.
func TestRESTConfigPassesAResolutionFailureThrough(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeKubeconfig(t, path, "prod")
	svc := newServiceAt(t, path, testInterval)
	start(t, svc)

	_, key, err := svc.RESTConfig("staging")

	assert.ErrorIs(t, err, ErrContextNotFound)
	assert.Empty(t, key)
}

// clientcmd falls back to the current context when the name is empty, which for a
// record naming no context would silently resolve to somebody else's cluster.
func TestResolveRejectsAnEmptyContextName(t *testing.T) {
	cfg := api.NewConfig()
	cfg.Clusters["prod-cluster"] = &api.Cluster{Server: "https://prod.invalid"}
	cfg.AuthInfos["prod-user"] = &api.AuthInfo{Token: "t"}
	cfg.Contexts["prod"] = &api.Context{Cluster: "prod-cluster", AuthInfo: "prod-user"}
	cfg.CurrentContext = "prod"

	_, err := resolve(cfg, "")
	assert.ErrorIs(t, err, ErrContextNotFound)
}

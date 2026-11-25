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

// Resolving one kube-context to the credentials a client connects with. Here rather
// than in the connection pool because this package is the only one that reads the
// kubeconfig, and the only one that calls clientcmd.
package kubeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// ErrContextNotFound reports that the kubeconfig holds no such context. A sentinel
// because the caller acts on it: a record whose context is gone is orphaned, which is
// a status to record rather than a failure to retry.
var ErrContextNotFound = errors.New("kube-context not found")

// ErrNotRead reports that the kubeconfig has not been read yet. Distinct from
// ErrContextNotFound because the pre-read config is empty, so every context looks
// absent — and a caller acting on that would record a live cluster as gone.
var ErrNotRead = errors.New("kubeconfig not read yet")

// RESTConfig resolves one context to credentials, and the key identifying them. The
// key changes when the credentials change; it is what the connection pool caches on.
func (s *Service) RESTConfig(contextName string) (*rest.Config, string, error) {
	cfg, read := s.Get()
	if !read {
		return nil, "", ErrNotRead
	}
	return restConfigFrom(cfg, contextName)
}

// restConfigFrom is RESTConfig over one snapshot, which is what keeps the key
// describing the credentials it accompanies: read twice, and a reload landing in
// between keys one snapshot's proxy URL onto another's credentials.
func restConfigFrom(cfg *api.Config, contextName string) (*rest.Config, string, error) {
	restCfg, err := resolve(cfg, contextName)
	if err != nil {
		return nil, "", err
	}
	return restCfg, fingerprint(restCfg, contextProxyURL(cfg, contextName)), nil
}

// resolve materializes the credentials for one context in cfg.
//
// cfg must be a config the loading rules produced. Loading is what resolves a
// relative certificate path against the kubeconfig's own directory, so a config
// assembled any other way yields CA and client-cert paths that cannot be opened.
func resolve(cfg *api.Config, contextName string) (*rest.Config, error) {
	// clientcmd reads an empty name as "the current context", which for a record that
	// names no context would resolve to whichever cluster the user last selected.
	if contextName == "" {
		return nil, fmt.Errorf("%w: no context named", ErrContextNotFound)
	}
	if _, ok := cfg.Contexts[contextName]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrContextNotFound, contextName)
	}

	// Deep-copied because clientcmd writes back through it: the merge takes the user's
	// *api.ExecConfig by pointer and then stamps InstallHint and the cluster's exec
	// extension onto it. Handed the service's snapshot, that would edit the config every
	// other reader shares — one user entry serving two clusters would end up with the
	// second one's audience under the first one's key.
	restCfg, err := clientcmd.NewNonInteractiveClientConfig(
		*cfg.DeepCopy(), contextName, &clientcmd.ConfigOverrides{}, nil,
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("resolve kube-context %q: %w", contextName, err)
	}
	return restCfg, nil
}

// fingerprint hashes everything that shapes the connection — who we connect as and how
// the transport is built — so a changed value is the signal to redial. The rule is
// every field clientcmd derives from the kubeconfig, so that deciding whether a
// difference "matters enough" never comes up; the one exclusion is what merely reaches
// the user, such as an exec plugin's install hint.
//
// It covers the *static* exec and auth-provider config rather than the token they mint:
// minting is the transport's job, but editing how a token is obtained must invalidate
// the connection.
//
// The context name is deliberately absent: two contexts pointing at one cluster with
// the same credentials share a connection.
func fingerprint(cfg *rest.Config, proxyURL string) string {
	h := sha256.New()
	// Length-prefixed, and every list and optional block preceded by its length, so the
	// hash reads as one parse rather than a run of values. Without the lengths an auth
	// provider named x with config {a: b} hashes the same as an exec plugin running x
	// with API version a and argument b — and the pool would then hand one context a
	// transport built for the other's credentials.
	writeBytes := func(b []byte) {
		fmt.Fprintf(h, "%d:", len(b))
		h.Write(b)
	}
	write := func(s string) { writeBytes([]byte(s)) }
	writeLen := func(n int) { fmt.Fprintf(h, "%d;", n) }

	// Alongside the rest.Config rather than in it: clientcmd compiles proxy-url into
	// cfg.Proxy, a func nothing can hash.
	write(proxyURL)

	t := cfg.TLSClientConfig
	for _, s := range []string{
		cfg.Host, cfg.APIPath, cfg.Username, cfg.Password,
		cfg.BearerToken, cfg.BearerTokenFile,
		t.ServerName, t.CAFile, t.CertFile, t.KeyFile,
		strconv.FormatBool(t.Insecure),
		strconv.FormatBool(cfg.DisableCompression),
	} {
		write(s)
	}
	writeBytes(t.CAData)
	writeBytes(t.CertData)
	writeBytes(t.KeyData)

	im := cfg.Impersonate
	write(im.UserName)
	write(im.UID)
	writeLen(len(im.Groups))
	for _, g := range im.Groups {
		write(g)
	}
	writeLen(len(im.Extra))
	for _, k := range slices.Sorted(maps.Keys(im.Extra)) {
		write(k)
		writeLen(len(im.Extra[k]))
		for _, v := range im.Extra[k] {
			write(v)
		}
	}

	if ap := cfg.AuthProvider; ap == nil {
		writeLen(0)
	} else {
		writeLen(1)
		write(ap.Name)
		writeLen(len(ap.Config))
		for _, k := range slices.Sorted(maps.Keys(ap.Config)) {
			write(k)
			write(ap.Config[k])
		}
	}

	if ep := cfg.ExecProvider; ep == nil {
		writeLen(0)
	} else {
		writeLen(1)
		write(ep.Command)
		write(ep.APIVersion)
		write(string(ep.InteractiveMode))
		// ProvideClusterInfo changes what the plugin is told, and Config is the
		// cluster's own exec extension — an audience, typically, which is how one user
		// entry serves several clusters.
		write(strconv.FormatBool(ep.ProvideClusterInfo))
		writeBytes(execConfig(ep.Config))
		writeLen(len(ep.Args))
		for _, a := range ep.Args {
			write(a)
		}
		writeLen(len(ep.Env))
		for _, e := range ep.Env {
			write(e.Name)
			write(e.Value)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// execConfig returns the bytes to hash for an exec plugin's per-cluster config. A
// kubeconfig's extension decodes to *runtime.Unknown, which holds the file's own
// bytes; anything else is encoded rather than skipped, so a shape this does not know
// cannot silently hash as absent.
func execConfig(obj runtime.Object) []byte {
	switch v := obj.(type) {
	case nil:
		return nil
	case *runtime.Unknown:
		return v.Raw
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Appendf(nil, "%#v", v)
		}
		return raw
	}
}

// contextProxyURL returns the proxy-url of the cluster contextName points at, or ""
// when the context, its cluster, or the field is absent.
func contextProxyURL(cfg *api.Config, contextName string) string {
	kubeCtx, ok := cfg.Contexts[contextName]
	if !ok || kubeCtx == nil {
		return ""
	}
	cluster, ok := cfg.Clusters[kubeCtx.Cluster]
	if !ok || cluster == nil {
		return ""
	}
	return cluster.ProxyURL
}

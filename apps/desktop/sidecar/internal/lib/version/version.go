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

// Package version is the build's version, stamped by the linker, and the
// User-Agent that names it. Nothing here reads the environment or a file: what a
// record says it was written by is what the binary was built as.
package version

import (
	"net/http"
	"runtime"
	"strings"
)

// Version is the release version, set with
// -X github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version.Version=<v>
// by scripts/build-sidecar.go when SIDECAR_VERSION is given. dev otherwise.
var Version = "dev"

// Product names the desktop app in a User-Agent.
const Product = "kubetail-desktop"

// UserAgent is the User-Agent of every request the sidecar sends, in client-go's
// shape: kubetail-desktop/<version> (<GOOS>/<GOARCH>).
func UserAgent() string {
	return Product + "/" + Version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
}

// Prefix puts UserAgent ahead of ua, a request's own User-Agent. A ua that
// UserAgent already leads comes back as is, so prefixing twice names the app
// once. One that names another build, as a sandboxed command may, is prefixed.
func Prefix(ua string) string {
	own := UserAgent()
	switch {
	case ua == "":
		return own
	case ua == own, strings.HasPrefix(ua, own+" "):
		return ua
	}
	return own + " " + ua
}

// Transport sends through base, http.DefaultTransport when nil, with Prefix
// applied to each request's User-Agent: an SDK's own agent follows Kubetail's.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return transport{base}
}

type transport struct{ base http.RoundTripper }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	ua := r.Header.Get("User-Agent")
	if prefixed := Prefix(ua); prefixed != ua {
		// A RoundTripper may not change the caller's request.
		r = r.Clone(r.Context())
		r.Header.Set("User-Agent", prefixed)
	}
	return t.base.RoundTrip(r)
}

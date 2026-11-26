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

package awsproxy

import (
	"net/http"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// sigV4 is the one signing scheme the proxy reads.
const sigV4 = "AWS4-HMAC-SHA256"

// credential is what a request's SigV4 credential names: the run's key id and
// the scope the client signed for. The signature itself is never read.
type credential struct {
	keyID, region, service string
	presigned              bool
}

// presignParams are a presigned URL's query parameters that carry the
// sandbox's signature, so none is forwarded.
var presignParams = []string{
	"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires", "X-Amz-SignedHeaders",
	"X-Amz-Signature", "X-Amz-Security-Token",
}

// credentialOf reads r's credential from Authorization or, for a presigned
// URL, from its query. A request carrying it in both places, or in neither,
// or under another scheme, has none.
func credentialOf(r *http.Request) (credential, bool) {
	auth := r.Header.Get("Authorization")
	query := r.URL.Query()
	presigned := query.Has("X-Amz-Algorithm") || query.Has("X-Amz-Credential") || query.Has("AWSAccessKeyId")
	switch {
	case auth != "" && presigned:
		return credential{}, false
	case presigned:
		if query.Get("X-Amz-Algorithm") != sigV4 {
			return credential{}, false
		}
		c, ok := scopeOf(query.Get("X-Amz-Credential"))
		c.presigned = true
		return c, ok
	}
	scheme, rest, _ := strings.Cut(auth, " ")
	if scheme != sigV4 {
		return credential{}, false
	}
	for field := range strings.SplitSeq(rest, ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(field), "Credential="); ok {
			return scopeOf(value)
		}
	}
	return credential{}, false
}

// scopeOf reads <id>/<date>/<region>/<service>/aws4_request.
func scopeOf(value string) (credential, bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 5 || parts[4] != "aws4_request" {
		return credential{}, false
	}
	return credential{keyID: parts[0], region: parts[2], service: parts[3]}, true
}

// token is the grant's token the key id carries, lower-cased as Token is; a
// key id without the mount's prefix carries none.
func (c credential) token() (string, bool) {
	hex, ok := strings.CutPrefix(c.keyID, sandbox.AWSKeyIDPrefix)
	return strings.ToLower(hex), ok && hex != ""
}

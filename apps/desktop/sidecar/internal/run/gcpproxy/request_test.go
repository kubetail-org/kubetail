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

package gcpproxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPathOnItsServiceIsReadBySegment(t *testing.T) {
	segs, why := checkPath(serviceNamed("storage"), "/storage/v1/b/bkt/o/dir%2Ffile.txt")
	assert.Empty(t, why)
	assert.Equal(t, []string{"storage", "v1", "b", "bkt", "o", "dir/file.txt"}, segs)

	segs, why = checkPath(serviceNamed("storage"), "/download/storage/v1/b/o/o/dir%2Ffile.txt")
	assert.Empty(t, why, "an object in a bucket named o")
	assert.Equal(t, "dir/file.txt", segs[len(segs)-1])

	segs, why = checkPath(serviceNamed("iam"), "/v1/projects/p/serviceAccounts/sa@p.iam.gserviceaccount.com:signBlob")
	assert.Empty(t, why)
	assert.Equal(t, "sa@p.iam.gserviceaccount.com:signBlob", segs[len(segs)-1])
}

func TestAPathOffTheTableIsRefused(t *testing.T) {
	cases := map[string]struct{ service, path string }{
		"an encoded slash":                        {"iam", "/v1/projects/p/serviceAccounts/sa%2Fkeys"},
		"an encoded slash in a bucket":            {"storage", "/storage/v1/b/a%2Fb"},
		"an encoded slash after a bucket named o": {"storage", "/storage/v1/b/o/x%2Fy"},
		"an encoded dot":                          {"compute", "/compute/v1/projects/%2e%2e/zones"},
		"a dot-dot":                               {"compute", "/compute/v1/../../v1/projects"},
		"a dot-dot object name":                   {"storage", "/storage/v1/b/bkt/o/%2E%2E"},
		"an empty segment":                        {"compute", "/compute/v1//projects"},
		"a trailing slash":                        {"compute", "/compute/v1/projects/"},
		"a bad escape":                            {"compute", "/compute/v1/projects/%zz"},
		"an XML API object":                       {"storage", "/bkt/object.txt"},
		"a batch path":                            {"storage", "/batch/storage/v1"},
		"a path off its service's list":           {"container", "/v2/projects"},
		"another service's path":                  {"logging", "/compute/v1/projects"},
		"the root":                                {"iam", "/"},
	}
	for name, tc := range cases {
		_, why := checkPath(serviceNamed(tc.service), tc.path)
		assert.NotEmpty(t, why, name)
	}
}

func TestARequestOffTheTableIsRefusedUnsent(t *testing.T) {
	r := newRig(t, roomy)
	assert.Equal(t, refusedPath, refusal(t, r.get(t, "http://storage.gcp.kubetail.invalid/bkt/object.txt")))
	assert.Equal(t, refusedPath, refusal(t, r.get(t, "http://iam.gcp.kubetail.invalid/v1/projects/p/serviceAccounts/sa%2Fkeys")))
	assert.Equal(t, refusedPath, refusal(t, r.get(t, "http://secretmanager.gcp.kubetail.invalid/v1/projects/p/secrets")))
	refusal(t, r.get(t, clusters+"?a=%zz"))
	assert.Empty(t, r.up.seen)
}

func TestACredentialInTheQueryIsRefused(t *testing.T) {
	r := newRig(t, roomy)
	for _, key := range []string{"access_token", "oauth_token", "key", "$key", "$access_token", "Key"} {
		assert.Equal(t, refusedQueryKey, refusal(t, r.get(t, clusters+"?"+key+"=x")), key)
	}
	assert.Empty(t, r.up.seen)
}

// A method override in the query would run a GET the classifier reads as a
// read as another method upstream, so it is never sent.
func TestAMethodOverrideInTheQueryIsRefused(t *testing.T) {
	r := newRig(t, roomy)
	for _, key := range []string{"$httpMethod", "httpMethod", "$HTTPMETHOD"} {
		assert.Equal(t, refusedMethodOverride, refusal(t, r.get(t, clusters+"/dev?"+key+"=DELETE")), key)
	}
	assert.Empty(t, r.asker.asked)
	assert.Empty(t, r.up.seen)
}

// A request names one project to bill at most, so the one classified is the
// one sent, whatever its spelling.
func TestAProjectBilledTwiceIsRefused(t *testing.T) {
	r := newRig(t, roomy)
	for _, query := range []string{"userProject=a&userProject=b", "userProject=a&$userproject=b", "USERPROJECT=a&userProject=a"} {
		assert.Equal(t, refusedBilledTwice, refusal(t, r.get(t, clusters+"/dev?"+query)), query)
	}
	twice := r.send(t, http.MethodGet, clusters+"/dev", r.grant.Token(), nil,
		http.Header{"X-Goog-User-Project": {"my-project", "other-project"}})
	assert.Equal(t, refusedBilledTwice, refusal(t, twice))
	assert.Empty(t, r.up.seen)
}

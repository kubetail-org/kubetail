// Copyright 2024 Andres Morey
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

package k8shelpers

import (
	"fmt"
	"net/http"
)

type BearerTokenRoundTripper struct {
	Transport http.RoundTripper
}

func (b *BearerTokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get token from context
	ctx := req.Context()
	token, ok := ctx.Value(K8STokenCtxKey).(string)
	if ok {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Call original transport
	return b.Transport.RoundTrip(req)
}

func NewBearerTokenRoundTripper(transport http.RoundTripper) *BearerTokenRoundTripper {
	return &BearerTokenRoundTripper{transport}
}

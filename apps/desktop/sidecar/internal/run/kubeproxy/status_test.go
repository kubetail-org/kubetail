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

package kubeproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A refusal is a Kubernetes Status, so each client prints it as its own error.
func TestAStatusIsAKubernetesStatus(t *testing.T) {
	w := httptest.NewRecorder()

	writeStatus(w, http.StatusForbidden, "kubetail: no")

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var st metav1.Status
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.Equal(t, metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure, Message: "kubetail: no", Reason: metav1.StatusReasonForbidden, Code: 403,
	}, st)
}

// Each code the proxy answers carries its own reason.
func TestEachCodeHasItsReason(t *testing.T) {
	for code, reason := range map[int]metav1.StatusReason{
		http.StatusUnauthorized:          metav1.StatusReasonUnauthorized,
		http.StatusForbidden:             metav1.StatusReasonForbidden,
		http.StatusRequestEntityTooLarge: metav1.StatusReasonRequestEntityTooLarge,
		http.StatusTooManyRequests:       metav1.StatusReasonTooManyRequests,
		http.StatusBadGateway:            metav1.StatusReasonInternalError,
		http.StatusServiceUnavailable:    metav1.StatusReasonServiceUnavailable,
	} {
		w := httptest.NewRecorder()
		writeStatus(w, code, "kubetail: x")
		var st metav1.Status
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
		assert.Equal(t, reason, st.Reason, code)
	}
}

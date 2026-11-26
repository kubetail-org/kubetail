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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// reasons is the Status reason of each code the proxy answers with.
var reasons = map[int]metav1.StatusReason{
	http.StatusUnauthorized:          metav1.StatusReasonUnauthorized,
	http.StatusForbidden:             metav1.StatusReasonForbidden,
	http.StatusRequestEntityTooLarge: metav1.StatusReasonRequestEntityTooLarge,
	http.StatusTooManyRequests:       metav1.StatusReasonTooManyRequests,
	http.StatusBadGateway:            metav1.StatusReasonInternalError,
	http.StatusServiceUnavailable:    metav1.StatusReasonServiceUnavailable,
}

// writeStatus answers with a Kubernetes Status carrying message, so each
// client prints it as its own error.
func writeStatus(w http.ResponseWriter, code int, message string) {
	body, _ := json.Marshal(metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure, Message: message, Reason: reasons[code], Code: int32(code),
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// Unauthorized answers 401 as a dead grant, or a request without its token,
// is answered: what a run's door answers a request no route takes.
func Unauthorized(w http.ResponseWriter) {
	writeStatus(w, http.StatusUnauthorized, "kubetail: unauthorized")
}

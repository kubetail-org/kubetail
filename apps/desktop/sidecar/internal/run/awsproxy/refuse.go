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
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"net/http"
	"strings"
)

// What the proxy answers in place of AWS.
const (
	noLogin        = "kubetail: no AWS login was found; run `aws sso login` outside the sandbox"
	loginChanged   = "kubetail: the AWS login changed while this request waited, so it was not sent"
	uploadRefusal  = "kubetail: the sandbox does not upload files to S3; do it outside the sandbox"
	upstreamFailed = "kubetail: AWS did not answer"
	uncheckable    = "kubetail: AWS's error could not be checked for the user's credentials, so it is not passed on"
)

// noLoginRefusal is what the proxy answers when the login resolved no
// credentials: the failure's own Refusal, or noLogin.
func noLoginRefusal(err error) string {
	var r Refusal
	if errors.As(err, &r) {
		return "kubetail: " + r.Refusal()
	}
	return noLogin
}

// refuse answers req with status and message in the shape its service's
// clients read, so the client prints the message. A call naming its operation
// in X-Amz-Target is answered in JSON, whichever its row.
func refuse(w http.ResponseWriter, req *request, status int, message string) {
	target := req.r.Header.Get("X-Amz-Target")
	shape := req.row.errors
	if target != "" {
		shape = errorsJSON
	}
	if shape == errorsJSON {
		contentType := req.r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(contentType, "application/x-amz-json"):
		case target != "":
			contentType = "application/x-amz-json-1.1"
		default:
			contentType = "application/json"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Amzn-Errortype", "AccessDeniedException")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"__type": "AccessDeniedException", "message": message})
		return
	}
	body := "<Code>AccessDenied</Code><Message>" + html.EscapeString(message) + "</Message>"
	switch shape {
	case errorsEC2:
		body = "<Response><Errors><Error>" + body + "</Error></Errors><RequestID>kubetail</RequestID></Response>"
	case errorsXML:
		body = "<Error>" + body + "</Error>"
	default:
		body = "<ErrorResponse><Error><Type>Sender</Type>" + body + "</Error></ErrorResponse>"
	}
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header + body))
}

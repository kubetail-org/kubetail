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

package azureproxy

import (
	"net/http"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// The labels of a request's details.
const (
	labelSubscription = "Subscription"
	labelGroup        = "Resource group"
	labelResource     = "Resource"
	labelIfMatch      = "If-Match"
	labelIfNoneMatch  = "If-None-Match"
)

// summary is the request in one line, naming the cloud platform.
func summary(method string, a armPath, v verdict, sub Subscription) string {
	in := sub.Name
	if a.group != "" {
		in = a.group
	}
	switch {
	case v.level == permissions.DataRead:
		return "Azure: read " + v.reads + " of " + a.resource() + " in " + in
	case method == http.MethodDelete && a.isGroup():
		return "Azure: delete resource group " + a.group + " in " + sub.Name
	}
	what := a.resource()
	if a.last != "" {
		what = strings.TrimPrefix(what+"/"+a.last, "/")
	}
	if what == "" {
		what = a.final()
	}
	return "Azure: " + method + " " + what + " in " + in
}

// details are what the classifier read from r: where it reaches, and each
// conditional header, which changes what a change does.
func details(r *http.Request, a armPath, sub Subscription) []runproxy.Detail {
	ds := []runproxy.Detail{{Label: labelSubscription, Value: sub.Name + " (" + sub.ID + ")"}}
	if a.group != "" {
		ds = append(ds, runproxy.Detail{Label: labelGroup, Value: a.group})
	}
	if res := a.resource(); res != "" {
		ds = append(ds, runproxy.Detail{Label: labelResource, Value: res})
	}
	if v := r.Header.Get("If-Match"); v != "" {
		ds = append(ds, runproxy.Detail{Label: labelIfMatch, Value: v})
	}
	if v := r.Header.Get("If-None-Match"); v != "" {
		ds = append(ds, runproxy.Detail{Label: labelIfNoneMatch, Value: v})
	}
	return ds
}

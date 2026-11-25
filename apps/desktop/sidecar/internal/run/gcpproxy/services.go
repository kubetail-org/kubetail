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

import "strings"

// routeSuffix ends every service's route: Kubetail picks the name, since gcloud
// takes an endpoint per service and a proxy of its own.
const routeSuffix = ".gcp.kubetail.invalid"

// service is one Google API the proxy reaches: the name gcloud uses in an
// endpoint override, the one upstream host, the path prefixes served, and,
// for a service whose answers hold secrets, the rule that blanks them in
// each object of an answer.
type service struct {
	name     string
	upstream string
	paths    []string
	blank    func(map[string]any)
}

// services is every API the proxy reaches. storage serves its XML API on the
// same host, where a GET of /<bucket>/<object> answers an object's contents
// with no alt=media, so its paths are the JSON API's alone.
var services = []service{
	{"compute", "compute.googleapis.com", []string{"/compute/"}, blankCompute},
	{"container", "container.googleapis.com", []string{"/v1/", "/v1beta1/"}, blankContainer},
	{"iam", "iam.googleapis.com", []string{"/v1/", "/v2/", "/v3/"}, nil},
	{"logging", "logging.googleapis.com", []string{"/v2/"}, nil},
	{"storage", "storage.googleapis.com", []string{"/storage/v1/", "/download/storage/v1/", "/upload/storage/v1/"}, nil},
	{"cloudresourcemanager", "cloudresourcemanager.googleapis.com", []string{"/v1/", "/v2/", "/v3/"}, nil},
}

// Services is each service's name, in the table's order.
func Services() []string {
	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.name
	}
	return names
}

// Host is the route a service is reached on.
func Host(name string) string { return name + routeSuffix }

// serviceOf is the service whose route host is, if any. The upstream comes
// from the table, never from the request.
func serviceOf(host string) (service, bool) {
	name, ok := strings.CutSuffix(host, routeSuffix)
	if !ok {
		return service{}, false
	}
	for _, s := range services {
		if s.name == name {
			return s, true
		}
	}
	return service{}, false
}

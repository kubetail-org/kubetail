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

package k8shelpers

type Key int

const (
	K8STokenCtxKey Key = iota
	K8SImpersonateCtxKey
)

// ImpersonateInfo carries the identity that downstream Kubernetes API calls
// should impersonate. Populated from the aggregation auth middleware when the
// cluster-api is fronted by the kube-apiserver aggregation layer.
type ImpersonateInfo struct {
	User   string
	Groups []string
	Extras map[string][]string
}

// ForEach yields one (key, value) pair per impersonation header that should
// be emitted for this identity, using the given header names. Yields nothing
// if info is nil or has an empty User.
func (info *ImpersonateInfo) ForEach(userKey, groupKey, extraPrefix string, fn func(key, value string)) {
	if info == nil || info.User == "" {
		return
	}
	fn(userKey, info.User)
	for _, g := range info.Groups {
		fn(groupKey, g)
	}
	for name, vals := range info.Extras {
		k := extraPrefix + name
		for _, v := range vals {
			fn(k, v)
		}
	}
}

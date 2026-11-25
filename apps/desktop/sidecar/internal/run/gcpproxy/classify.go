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
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// kind is what the proxy does with a request.
type kind int

const (
	read    kind = iota // forwarded unasked
	asks                // decided by the grant
	refused             // never sent
)

// verdict is a request's kind and access level, the action decided when it
// is decided, and why it is refused.
type verdict struct {
	kind  kind
	level permissions.AccessLevel
	act   permissions.Action
	why   string
}

const (
	refusedCredential = "kubetail: this request's answer is a credential, so it was not sent."
	refusedResumable  = "kubetail: a resumable upload answers a session that uploads with no token, so it was not sent."
)

// classify reads a request on the service named svc, its path's decoded
// segments, its query, its headers and its body: the first rule that applies
// decides. A
// request the grant decides carries its action; a read carries none, since
// it skips the decision.
func classify(svc, method string, segs []string, q url.Values, header http.Header, body []byte) verdict {
	last := segs[len(segs)-1]
	res := resource(svc, segs)
	var l permissions.AccessLevel
	switch {
	case answersACredential(svc, method, res):
		return verdict{kind: refused, level: permissions.Reach, why: refusedCredential}
	case svc == "storage" && hasSystemValue(q, "resumable", "uploadtype", "upload_protocol"):
		return verdict{kind: refused, why: refusedResumable}
	case method == http.MethodGet && readsData(svc, segs, q):
		l = permissions.DataRead
	case method == http.MethodGet || method == http.MethodHead:
		return verdict{kind: read, level: permissions.Read}
	case method == http.MethodPost && (isMethod(last, "getIamPolicy") || isMethod(last, "testIamPermissions")):
		return verdict{kind: read, level: permissions.Read}
	case method == http.MethodPost && svc == "logging" && slices.Equal(segs, []string{"v2", "entries:list"}):
		return verdict{kind: read, level: permissions.Read}
	case changesAccessControl(svc, method, res), svc == "storage" && changesStorageAccess(res, q, header, body):
		l = permissions.AccessControl
	case isDestructive(svc, method, res):
		l = permissions.Destructive
	default:
		l = permissions.Write
	}
	return verdict{kind: asks, level: l, act: action(svc, method, segs, q, header.Get("X-Goog-User-Project"), body, l)}
}

// ordinary are the changes a mode or a rule may allow, by service, then
// resource, then operation. Any other write asks each time. Adding a row is
// a security change.
var ordinary = map[string]map[string][]string{
	"compute": {"instances": {"start", "reset", "setlabels"}},
}

// action is the action a request the grant decides carries. Its scope is
// the project named as an id, then the location, both in lower case; a
// project named any other way and a path that names none give no scope. A
// body that names another project or is not JSON, and a request billed to
// another project, ask each time.
func action(svc, method string, segs []string, q url.Values, quota string, body []byte, l permissions.AccessLevel) permissions.Action {
	p := parsePath(svc, segs)
	act := permissions.Action{
		Service: svc, Operation: strings.ToLower(cmp.Or(p.method, method)), Resource: strings.Join(p.types, "/"),
		Name: p.name, AccessLevel: l, AccountName: p.project, Summary: summary(svc, method, segs),
	}
	if projectID.MatchString(p.project) {
		act.Scope = p.project
		if p.location != "" {
			act.Scope += "/" + p.location
		}
		// Reaching past the project keeps its scope, so its mode and rules still
		// refuse, and asks each time.
		act.Once = foreign(body, p.project) || billsAnotherProject(p.project, quota, q)
	}
	if l == permissions.Write && (method != http.MethodPost || !slices.Contains(ordinary[svc][act.Resource], act.Operation)) {
		act.Once = true
	}
	return act
}

// projectID is a project named as an id: a project number and "-" are not
// one.
var projectID = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// locationWords are the collections a location is named under.
var locationWords = []string{"zones", "regions", "locations"}

// apiPath is what action reads of a path past its API's version.
type apiPath struct {
	project, location string
	types             []string // each collection's type, in lower case
	name              string   // the last collection member named
	method            string   // the custom method, "" for none
}

// parsePath reads segs as collection and member pairs past the version: the
// first project, the location right after it, each other collection's type,
// and a custom method, after ":" or, on compute, a segment of its own after a
// member.
func parsePath(svc string, segs []string) apiPath {
	rest := pastVersion(segs)
	var p apiPath
	afterMember := false
	for i := 0; i < len(rest); i += 2 {
		coll := rest[i]
		if svc == "compute" && coll == "global" {
			i--
			continue
		}
		if i+1 == len(rest) {
			coll, m, ok := strings.Cut(coll, ":")
			switch {
			case ok:
				p.method = m
				p.types = append(p.types, strings.ToLower(coll))
			case svc == "compute" && afterMember:
				p.method = coll
			default:
				p.types = append(p.types, strings.ToLower(coll))
			}
			break
		}
		member, m, ok := strings.Cut(rest[i+1], ":")
		if ok {
			p.method = m
		}
		switch {
		case coll == "projects" && p.project == "":
			p.project = strings.ToLower(member)
			afterMember = false
			continue
		case slices.Contains(locationWords, coll) && p.location == "" && p.project != "" && len(p.types) == 0:
			p.location = strings.ToLower(member)
			afterMember = false
			continue
		}
		p.types = append(p.types, strings.ToLower(coll))
		p.name, afterMember = member, true
	}
	if len(p.types) == 0 && p.project != "" {
		p.types, p.name = []string{"projects"}, p.project
	}
	return p
}

// foreign is whether body may name a project other than project: a body
// that is not JSON, or one whose keys or strings, read token by token, hold
// projects/ followed by another name. An empty body names nothing.
func foreign(body []byte, project string) bool {
	if len(body) == 0 {
		return false
	}
	return runproxy.AnyJSONString(body, func(s string) bool { return namesAnotherProject(s, project) })
}

// storageAccessKeys are the keys of a bucket's or an object's metadata that
// say who may reach it.
var storageAccessKeys = []string{"acl", "defaultObjectAcl", "iamConfiguration"}

// changesStorageAccess is whether a storage change says who may reach a bucket
// or an object: a write under a bucket's acl or defaultObjectAcl or an
// object's acl, a predefined ACL, or metadata naming an access key. Metadata
// that cannot be read is taken to name one.
func changesStorageAccess(segs []string, q url.Values, header http.Header, body []byte) bool {
	// storage/v1/b/{bucket}/acl, …/defaultObjectAcl, …/o/{object}/acl: a
	// bucket or an object may itself be named acl.
	n := len(segs)
	if n > 4 && segs[2] == "b" && (segs[4] == "acl" || segs[4] == "defaultObjectAcl") ||
		n > 6 && segs[2] == "b" && segs[4] == "o" && segs[6] == "acl" {
		return true
	}
	if len(systemValues(q, "predefinedacl")) > 0 || len(systemValues(q, "predefineddefaultobjectacl")) > 0 {
		return true
	}
	keys, ok := storageMetadata(q, header, body)
	return !ok || slices.ContainsFunc(storageAccessKeys, func(k string) bool { _, named := keys[k]; return named })
}

// storageMetadata is the top-level keys of the metadata a storage change
// sends: its JSON body, or a multipart upload's first part. A media upload
// and an empty body send none. False is metadata that does not decode.
func storageMetadata(q url.Values, header http.Header, body []byte) (map[string]json.RawMessage, bool) {
	if hasSystemValue(q, "media", "uploadtype") || len(body) == 0 {
		return nil, true
	}
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if mediaType == "multipart/related" {
		part, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).NextPart()
		if err != nil {
			return nil, false
		}
		if body, err = io.ReadAll(part); err != nil {
			return nil, false
		}
	}
	var keys map[string]json.RawMessage
	return keys, json.Unmarshal(body, &keys) == nil
}

// billsAnotherProject is whether a request charges a project other than
// project: its quota project, or the userProject parameter.
func billsAnotherProject(project, quota string, q url.Values) bool {
	for _, billed := range append([]string{quota}, systemValues(q, "userproject")...) {
		if billed != "" && strings.ToLower(billed) != project {
			return true
		}
	}
	return false
}

// namesAnotherProject is whether s holds projects/ followed by a name other
// than project. s is lowered once: lowering can change its length in bytes.
func namesAnotherProject(s, project string) bool {
	s = strings.ToLower(s)
	for {
		i := strings.Index(s, "projects/")
		if i < 0 {
			return false
		}
		s = s[i+len("projects/"):]
		end := strings.IndexFunc(s, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.')
		})
		if end < 0 {
			end = len(s)
		}
		if s[:end] != project {
			return true
		}
	}
}

// resource is segs past storage's upload or download prefix, so a rule that
// reads a segment by its place reads it under every prefix.
func resource(svc string, segs []string) []string {
	if svc == "storage" && (segs[0] == "upload" || segs[0] == "download") {
		return segs[1:]
	}
	return segs
}

// isMethod is whether seg calls name: the whole segment, as compute spells a
// custom method, or its suffix after ":", as the other APIs do.
func isMethod(seg, name string) bool {
	return seg == name || strings.HasSuffix(seg, ":"+name)
}

// answersACredential is whether the request's answer is a credential: a
// service account key or a token or signature made with one, an HMAC key, an
// OAuth client's secret or a SCIM token.
func answersACredential(svc, method string, segs []string) bool {
	last := segs[len(segs)-1]
	for _, m := range []string{"signBlob", "signJwt", "generateAccessToken", "generateIdToken"} {
		if strings.HasSuffix(last, ":"+m) {
			return true
		}
	}
	switch svc {
	case "iam":
		return method == http.MethodPost && last == "keys" ||
			under(segs, "oauthClients", "credentials") || under(segs, "scimTenants", "tokens")
	case "storage":
		return method == http.MethodPost && len(segs) >= 5 && segs[2] == "projects" && segs[4] == "hmacKeys"
	}
	return false
}

// under is whether segs holds collection somewhere below parent.
func under(segs []string, parent, collection string) bool {
	i := slices.Index(segs, parent)
	return i >= 0 && slices.Contains(segs[i+1:], collection)
}

// readsData is whether a GET answers stored data or what a guest wrote: an
// object's contents, a serial port, guest attributes or a screenshot.
func readsData(svc string, segs []string, q url.Values) bool {
	if hasSystemValue(q, "media", "alt") {
		return true
	}
	switch svc {
	case "storage":
		return segs[0] == "download"
	case "compute":
		return slices.Contains([]string{"serialPort", "getGuestAttributes", "screenshot"}, segs[len(segs)-1])
	}
	return false
}

// changesAccessControl is whether a change is to who may do what: a
// setIamPolicy, a storage IAM PUT, a bucket's or a managed folder's, and
// every change on the iam service.
func changesAccessControl(svc, method string, segs []string) bool {
	n := len(segs)
	switch {
	case svc == "iam":
		return true
	case method == http.MethodPost:
		return isMethod(segs[n-1], "setIamPolicy")
	case method == http.MethodPut:
		return svc == "storage" && segs[n-1] == "iam" && segs[n-2] != "o"
	}
	return false
}

// isDestructive is whether a change is on the destructive list: a DELETE of a
// project, a GKE cluster or a bucket.
func isDestructive(svc, method string, segs []string) bool {
	n := len(segs)
	switch method {
	case http.MethodDelete:
		parent := ""
		if n >= 2 {
			parent = segs[n-2]
		}
		switch svc {
		case "cloudresourcemanager":
			return n == 3 && parent == "projects"
		case "container":
			return parent == "clusters"
		case "storage":
			return n == 4 && parent == "b"
		}
	}
	return false
}

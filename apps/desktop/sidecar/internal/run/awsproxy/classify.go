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
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// operation is what the classifier read of a request.
type operation struct {
	name      string                  // a query or JSON operation; "" for a REST row
	operation string                  // name, or a REST row's method
	level     permissions.AccessLevel // "" for a refusal
	once      bool                    // a write ordinary does not name
	foreign   bool                    // it names a resource another account may own
	refusal   string                  // why it is not sent, "" to send it
	bucket    string                  // an S3 request's bucket, "" for none
	resource  string                  // bucket or object on S3, "" elsewhere
	object    string                  // the S3 bucket or bucket/key named, "" elsewhere
}

// s3MetadataQuery are the subresources of an S3 key whose GET answers
// metadata, not the object's data.
var s3MetadataQuery = []string{"acl", "attributes", "legal-hold", "retention", "tagging", "uploadId"}

// s3AccessQuery are the subresources of a bucket or key that say who may
// reach it: a change to one is an access control change, a delete included.
var s3AccessQuery = []string{"acl", "policy", "publicAccessBlock", "ownershipControls"}

// ordinary are the changes a mode or a rule may allow, by service, beyond
// S3's object PUT (ordinaryPut). Any other change asks each time. Adding a
// row is a security change.
var ordinary = map[string][]string{
	"ec2":                  {"StartInstances", "CreateTags", "CreateVolume", "CreateSnapshot"},
	"elasticloadbalancing": {"RegisterTargets", "AddTags"},
	"ecr":                  {"InitiateLayerUpload", "UploadLayerPart", "CompleteLayerUpload", "PutImage"},
	"logs":                 {"CreateLogGroup", "CreateLogStream", "PutLogEvents"},
}

// accessControlWords mark a change of who may do what in an operation's name.
var accessControlWords = []string{"Policy", "Permission", "Grant"}

// classify reads one request to the row service names: refused (reach for a
// credential), then data read, destructive, a read, and a write for the rest,
// so what it cannot name asks. A change on iam, or one whose name holds a
// accessControlWords word, is an access control change. A REST row classifies from
// the method, path, query and headers alone.
func classify(service string, r *http.Request, body []byte) operation {
	row := services[service]
	if row.protocol == protocolREST {
		return classifyREST(service, r)
	}
	op := operation{level: permissions.Write}
	// The operation is read only where the row's protocol names it, so the one
	// classified is the one AWS runs.
	target := r.Header.Get("X-Amz-Target")
	params := queryParams(r, body)
	actions, hasAction := params["Action"]
	if target != "" && (hasAction || row.protocol == protocolQuery) || hasAction && row.protocol == protocolJSON ||
		len(actions) > 1 {
		op.level = ""
		op.refusal = "kubetail: this request names its operation where " + service +
			" does not read it, or twice, so the sandbox does not send it"
		return op
	}
	if target != "" {
		_, op.name, _ = strings.Cut(target, ".")
	} else if len(actions) == 1 {
		op.name = actions[0]
	}
	op.operation = op.name
	op.foreign = foreign(service, op.name, params, body)
	switch {
	case op.name == "":
	case listed(row.refused, op.name) || service == "sts" && op.name != "GetCallerIdentity":
		op.level, op.refusal = permissions.Reach, credentialRefusal(service+":"+op.name)
		return op
	case listed(row.secrets, op.name) || secretRead(service, op.name, params, body):
		op.level = permissions.DataRead
		return op
	case hasAnyPrefix(op.name, "Delete", "Terminate") || listed(row.destroys, op.name):
		op.level = permissions.Destructive
	case hasAnyPrefix(op.name, "Describe", "Get", "List") || listed(row.reads, op.name):
		if op.foreign {
			// A write though it reads: another account's log would record it.
			op.once = true
			return op
		}
		op.level = permissions.Read
		return op
	}
	if service == "iam" || slices.ContainsFunc(accessControlWords, func(w string) bool { return strings.Contains(op.name, w) }) {
		op.level = permissions.AccessControl
		return op
	}
	op.once = op.level == permissions.Write && !slices.Contains(ordinary[service], op.name)
	return op
}

// classifyREST reads a REST row's request: a GET or HEAD reads, a DELETE is
// destructive, and S3 has its own subresources.
func classifyREST(service string, r *http.Request) operation {
	op := operation{level: permissions.Write, operation: r.Method}
	method, query := r.Method, r.URL.Query()
	s3 := service == "s3"
	var key string
	if s3 {
		op.bucket, key, _ = strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		switch {
		case key != "":
			op.resource, op.object = "object", op.bucket+"/"+key
		case op.bucket != "":
			op.resource, op.object = "bucket", op.bucket
		}
	}
	switch {
	case s3 && query.Has("session"):
		op.level, op.refusal = permissions.Reach, credentialRefusal("S3's CreateSession")
	case s3 && method == http.MethodGet && key != "" && !slices.ContainsFunc(s3MetadataQuery, query.Has),
		s3 && method == http.MethodPost && query.Has("select"):
		op.level = permissions.DataRead
	case s3 && method != http.MethodGet && method != http.MethodHead && changesS3Access(r):
		op.level = permissions.AccessControl
	case method == http.MethodDelete, s3 && method == http.MethodPost && query.Has("delete"):
		op.level = permissions.Destructive
	case method == http.MethodGet || method == http.MethodHead:
		op.level = permissions.Read
	default:
		op.once = !(s3 && key != "" && ordinaryPut(r))
	}
	return op
}

// changesS3Access is whether an S3 request names who may reach its bucket or
// key: a subresource of s3AccessQuery, or an ACL or grant header.
func changesS3Access(r *http.Request) bool {
	if slices.ContainsFunc(s3AccessQuery, r.URL.Query().Has) {
		return true
	}
	for name := range r.Header {
		name = http.CanonicalHeaderKey(name)
		if name == "X-Amz-Acl" || strings.HasPrefix(name, "X-Amz-Grant-") {
			return true
		}
	}
	return false
}

// ordinaryPut is S3's ordinary change: a PUT of an object whose query holds no
// key but x-id, carrying no copy source. One carrying an ACL or a grant is an
// access control change, classified before it.
func ordinaryPut(r *http.Request) bool {
	if r.Method != http.MethodPut {
		return false
	}
	for k := range r.URL.Query() {
		if k != "x-id" {
			return false
		}
	}
	for name := range r.Header {
		name = http.CanonicalHeaderKey(name)
		if name == "X-Amz-Copy-Source" {
			return false
		}
	}
	return true
}

// credentialRefusal is the answer to an operation whose answer is a
// credential.
func credentialRefusal(what string) string {
	return "kubetail: " + what + " answers a credential, so the sandbox does not send it"
}

// unmaskable are the log reads that take an unmask.
var unmaskable = []string{"GetLogEvents", "FilterLogEvents", "GetLogRecord", "StartQuery"}

// secretRead is a data read rule that reads the request's parameters: EC2's
// attribute read that can answer user data, and a log read that unmasks.
func secretRead(service, op string, params url.Values, body []byte) bool {
	switch {
	case service == "ec2" && op == "DescribeInstanceAttribute":
		attrs := params["Attribute"]
		return len(attrs) != 1 || attrs[0] == "userData"
	case service == "logs" && slices.Contains(unmaskable, op):
		return mentions(body, "unmask")
	}
	return false
}

// foreign is a request that names a resource another account may own: an ECR
// registry, or a template AWS fetches from a URL. A read of one is a write,
// since that account's log would record it.
func foreign(service, op string, params url.Values, body []byte) bool {
	switch {
	case service == "ecr":
		return mentions(body, "registryid")
	case service == "cloudformation" && (op == "ValidateTemplate" || op == "EstimateTemplateCost"):
		return params.Has("TemplateURL")
	}
	return false
}

// mentions is whether any key or string in the JSON body holds word, in any
// case. A body that is not one JSON value mentions everything.
func mentions(body []byte, word string) bool {
	return anyToken(body, func(tok json.Token) bool {
		s, ok := tok.(string)
		return ok && strings.Contains(strings.ToLower(s), word)
	})
}

// anyToken is whether hit holds for any key or value of body, a number as a
// json.Number: false for an empty body, true for one that is not one JSON
// value, whitespace alone or two values included. Every token is read, so a
// duplicate key or an escape cannot hide one.
func anyToken(body []byte, hit func(json.Token) bool) bool {
	if len(body) == 0 {
		return false
	}
	if !json.Valid(body) {
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return false
		}
		if _, ok := tok.(json.Delim); !ok && hit(tok) {
			return true
		}
	}
}

// queryParams are a query row's parameters: the URL's, and a form body's.
// A form that does not parse names no operation, so it asks.
func queryParams(r *http.Request, body []byte) url.Values {
	params := r.URL.Query()
	if runproxy.IsForm(r.Header.Get("Content-Type")) {
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return url.Values{}
		}
		for k, v := range form {
			params[k] = append(params[k], v...)
		}
	}
	return params
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s, p) })
}

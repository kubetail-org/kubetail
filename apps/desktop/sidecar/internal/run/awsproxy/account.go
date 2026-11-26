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
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// globalRegion is the region segment of a row with one host for every
// region.
const globalRegion = "global"

// accountOf is the account creds carry, "" for none Kubetail can trust: AWS's
// twelve digits, or nothing.
func accountOf(creds aws.Credentials) string {
	if len(creds.AccountID) != 12 || strings.Trim(creds.AccountID, "0123456789") != "" {
		return ""
	}
	return creds.AccountID
}

// actionScope is req's scope for the login creds: its account, then its region,
// or global for a row with one host. A login whose account Kubetail cannot
// trust has scope "".
func actionScope(req *request, creds aws.Credentials) string {
	account := accountOf(creds)
	if account == "" {
		return ""
	}
	region := strings.ToLower(req.cred.region)
	if !strings.Contains(req.row.hostFormat, "{region}") {
		region = globalRegion
	}
	return account + "/" + region
}

// foreignTo is whether req reaches past the login creds' account: a bucket it
// does not own, or another account it names. Its scope stays the account's, so
// that account's mode and rules still refuse it, and it asks each time.
func foreignTo(req *request, creds aws.Credentials) bool {
	account := accountOf(creds)
	return req.foreign || req.op.foreign || account != "" && namesAnotherAccount(req, account)
}

// namesAnotherAccount is whether req names an account other than account: a
// query or form value, a key, string or number of a JSON body, or the KMS key
// or expected bucket owner header holding a run of exactly twelve digits that
// is not it. Any other
// body is foreign, but for a plain S3 object PUT's, which S3 reads no account
// from; an empty one names nothing.
func namesAnotherAccount(req *request, account string) bool {
	r := req.r
	for _, h := range []string{"X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id", "X-Amz-Expected-Bucket-Owner"} {
		if otherAccount(r.Header.Get(h), account) {
			return true
		}
	}
	if valuesName(r.URL.Query(), account) {
		return true
	}
	if len(req.body) == 0 || req.cred.service == "s3" && req.op.resource == "object" && ordinaryPut(r) {
		return false
	}
	if runproxy.IsForm(r.Header.Get("Content-Type")) {
		form, err := url.ParseQuery(string(req.body))
		return err != nil || valuesName(form, account)
	}
	return jsonNames(req.body, account)
}

func valuesName(values url.Values, account string) bool {
	for _, vs := range values {
		for _, v := range vs {
			if otherAccount(v, account) {
				return true
			}
		}
	}
	return false
}

// jsonNames is whether any key, string or number of a JSON body names an
// account other than account. A body that is not JSON names one.
func jsonNames(body []byte, account string) bool {
	return anyToken(body, func(tok json.Token) bool {
		switch v := tok.(type) {
		case string:
			return otherAccount(v, account)
		case json.Number:
			return otherAccount(v.String(), account)
		}
		return false
	})
}

// otherAccount is whether s holds a run of exactly twelve digits that is not
// account.
func otherAccount(s, account string) bool {
	for run := range strings.FieldsFuncSeq(s, func(c rune) bool { return c < '0' || c > '9' }) {
		if len(run) == 12 && run != account {
			return true
		}
	}
	return false
}

// action is the request as the decision reads it, in the account of creds.
func (req *request) action(creds aws.Credentials) permissions.Action {
	return permissions.Action{
		Scope: actionScope(req, creds), AccountName: creds.AccountID,
		Service: req.cred.service, Operation: req.op.operation, Resource: req.op.resource, Name: req.op.object,
		AccessLevel: req.op.level, Once: req.op.once || foreignTo(req, creds), Summary: req.summary(),
	}
}

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

package safe_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// Each case names what must go and what must survive: a renderer that returns "error" is
// safe and useless, so every shape is asserted from both sides.
func TestSafeStripsCredentialShapesAndKeepsTheDiagnostic(t *testing.T) {
	cases := []struct {
		name string
		in   string
		gone []string
		kept []string
	}{
		{
			name: "a URL's query and userinfo",
			in: `Get "https://svc:hunter2@api.example.com/readyz?token=SEKRIT&verbose=true": ` +
				`dial tcp 10.0.0.1:443: connect: connection refused`,
			gone: []string{"SEKRIT", "hunter2", "token="},
			kept: []string{"api.example.com", "/readyz", "connection refused"},
		},
		{
			name: "a URL's userinfo without a query",
			in:   `dial "https://svc:hunter2@api.example.com/healthz": no route to host`,
			gone: []string{"hunter2", "svc:"},
			kept: []string{"api.example.com", "/healthz", "no route to host"},
		},
		{
			name: "a URL whose userinfo holds a field or flag shape keeps its @ for the URL rule",
			in: "Get https://alice:secret,token:more@host/path: refused\n" +
				"Get https://alice:secret--token=more@host/path: refused\n",
			gone: []string{"alice", "secret", "more"},
			kept: []string{"[redacted]@host/path", "refused"},
		},
		{
			name: "an echoed Authorization header",
			in: "oauth2: cannot fetch token: 401 Unauthorized\n" +
				"Authorization: Basic SEKRIT\n" +
				`Response: {"error":"invalid_grant"}`,
			gone: []string{"SEKRIT"},
			kept: []string{"401 Unauthorized", "invalid_grant"},
		},
		{
			name: "a header written with = rather than :",
			in:   "request rejected\nAuthorization=Basic SEKRIT",
			gone: []string{"SEKRIT"},
			kept: []string{"request rejected", "Authorization"},
		},
		{
			name: "a header whose value carries its own =",
			in:   "Cookie=session=SEKRIT; Path=/\nunexpected redirect",
			gone: []string{"SEKRIT"},
			kept: []string{"unexpected redirect", "Cookie"},
		},
		{
			name: "an echoed Set-Cookie header",
			in:   "Set-Cookie: session=SEKRIT; Path=/\nunexpected redirect",
			gone: []string{"SEKRIT"},
			kept: []string{"unexpected redirect"},
		},
		{
			name: "an echoed API-key header",
			in:   "anthropic: 401 authentication_error\nx-api-key: sk-ant-SEKRIT\nrequest-id: req_1",
			gone: []string{"SEKRIT", "sk-ant"},
			kept: []string{"401 authentication_error", "req_1"},
		},
		{
			name: "Azure's spelling of the same header",
			in:   "api-key=SEKRITSEKRITSEKRIT\nrequest rejected",
			gone: []string{"SEKRITSEKRITSEKRIT"},
			kept: []string{"request rejected", "api-key"},
		},
		{
			name: "a bare key in free text",
			in:   "the key sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789 was rejected",
			gone: []string{"abcdefghijklmnopqrstuvwxyz0123456789"},
			kept: []string{"the key", "was rejected"},
		},
		{
			name: "a bearer token in free text",
			in:   "the server rejected Bearer SEKRIT as expired",
			gone: []string{"SEKRIT"},
			kept: []string{"the server rejected", "as expired"},
		},
		{
			name: "a kubeconfig's credential fields",
			in: "clusters:\n- cluster:\n    server: https://k8s.example.com\n" +
				"    certificate-authority-data: LS0tQ0VSVA==\n" +
				"users:\n- name: prod\n  user:\n" +
				"    client-certificate-data: LS0tQ0VSVA==\n" +
				"    client-key-data: LS0tS0VZ\n    token: eyJhLmIuYw\n",
			gone: []string{"LS0tS0VZ", "eyJhLmIuYw"},
			kept: []string{"server: https://k8s.example.com", "LS0tQ0VSVA==", "name: prod"},
		},
		{
			name: "block scalars with every header shape",
			in: "user:\n  token: | # credential\n    eyJhLmIuYw\n  next1: kept\n" +
				"  password: >   \n    p1opaque\n  next2: kept\n" +
				"  client-key-data: |2-\n    LS0tS0VZ\n\n    MOREKEY\n  next3: kept\n",
			gone: []string{"eyJhLmIuYw", "p1opaque", "LS0tS0VZ", "MOREKEY"},
			kept: []string{
				"token: [redacted]", "password: [redacted]", "client-key-data: [redacted]",
				"next1: kept", "next2: kept", "next3: kept",
			},
		},
		{
			name: "a .netrc on one line, every token order and a quoted login",
			in: "machine h login \"first last\" password opaque1\n" +
				"default password opaque2\nmachine h password hunter2 login bob\n",
			gone: []string{"opaque1", "opaque2", "hunter2"},
			kept: []string{`login "first last"`, "default", "login bob"},
		},
		{
			name: "a quoted .netrc password is taken whole, its escaped quote no close",
			in:   "machine api.example.com\n  login bob\n  password \"pre\\\"TAILsecret\"\n",
			gone: []string{"TAILsecret", "pre"},
			kept: []string{"api.example.com", "login bob", "password [redacted]"},
		},
		{
			name: "a .netrc over lines with a quoted, blank-holding password",
			in:   "machine api.example.com\n  login bob\n  password \"a b c\"\n",
			gone: []string{"a b c"},
			kept: []string{"api.example.com", "login bob", "password [redacted]"},
		},
		{
			name: "a quoted .netrc password whose contents match another rule",
			in: "machine h login bob password \"first second --token=third\"\n" +
				"machine h2 login ann password \"Authorization: Basic fourth\"\n" +
				"machine h3 login cy password \"fifth sixth,token:seventh\"\n",
			gone: []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "--token", "Authorization"},
			kept: []string{"login bob", "login ann", "login cy", "password [redacted]"},
		},
		{
			name: "a .netrc with the value on the line after the word",
			in:   "machine h login bob password\n  opaque3\ndefault\n  password d2opaque\n",
			gone: []string{"opaque3", "d2opaque"},
			kept: []string{"login bob", "default", "password [redacted]"},
		},
		{
			name: "a line opening with the word loses the word after it",
			in:   "password must be 8 characters\nEnter your\npassword\nthen press\n",
			gone: []string{"must", "then"},
			kept: []string{"password [redacted] be 8 characters", "Enter your", "press"},
		},
		{
			name: "a command line as a shell wrote it",
			in:   `kubectl --password=-opaque1 --password="first"second --token t0opaque --token --server=y --token=x1`,
			gone: []string{"-opaque1", `"first"second`, "t0opaque", "=x1"},
			kept: []string{"--password=[redacted]", "--token [redacted]", "--token=[redacted]", "--token --server=y"},
		},
		{
			name: "flattened argv, as ps prints it",
			in:   "kubectl --password first second",
			gone: []string{"first"},
			kept: []string{"second"},
		},
		{
			name: "api-key on a command line, shadowed by headerRE",
			in:   "kubectl --api-key=secretsecret --server=y",
			gone: []string{"secretsecret", "--server=y"},
			kept: []string{"--api-key", "[redacted]"},
		},
		{
			name: "a flow scalar wrapped onto a deeper line",
			in:   "user:\n  token: \"alpha1\n    beta2\"\n  next: kept\n",
			gone: []string{"alpha1", "beta2"},
			kept: []string{"token: [redacted]", "next: kept"},
		},
		{
			name: "a quoted value closed on its own line, escapes inside",
			in: "token: \"has \\\" escaped\"\nnext1: kept\n" +
				"token: 'it''s'\nnext2: kept\n" +
				"token: \"ends with backslash\\\\\"\nnext3: kept\n",
			gone: []string{"escaped", "it''s", "backslash"},
			kept: []string{"token: [redacted]\nnext1: kept", "token: [redacted]\nnext2: kept", "token: [redacted]\nnext3: kept"},
		},
		{
			name: "a quoted key's depth is its opening quote, so a one-space block is its value",
			in:   "user:\n  \"token\": |1\n   supersecretvalue\n  next: kept\n",
			gone: []string{"supersecretvalue"},
			kept: []string{`"token": [redacted]`, "next: kept"},
		},
		{
			name: "a credential field under a YAML sequence marker",
			in:   "users:\n- token: opaquesecret\n- password: |\n    blockopaque\n- name: prod\n",
			gone: []string{"opaquesecret", "blockopaque"},
			kept: []string{"- token: [redacted]", "- password: [redacted]", "- name: prod"},
		},
		{
			name: "a sequence item's other fields sit at the name's depth and survive",
			in:   "- token: opaquesecret\n  name: prod\n  user:\n    server: https://api.example.com\n",
			gone: []string{"opaquesecret"},
			kept: []string{"- token: [redacted]", "name: prod", "server: https://api.example.com"},
		},
		{
			name: "a listed name over a nested map loses the map",
			in:   "user:\n  token:\n    file: /path/to/token\n  next: kept\n",
			gone: []string{"/path/to/token"},
			kept: []string{"token: [redacted]", "next: kept"},
		},
		{
			name: "a service-account JSON as cat prints it",
			in: `{` + "\n" + `  "private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADAN\n-----END PRIVATE KEY-----\n",` + "\n" +
				`  "client_email": "sa@p.iam"` + "\n}",
			gone: []string{"MIIEvQIBADAN", "BEGIN"},
			kept: []string{`"private_key": [redacted]`, `"client_email": "sa@p.iam"`},
		},
		{
			name: "an .aws/credentials file",
			in: "[default]\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\n" +
				"aws_secret_access_key = wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY\n" +
				"region = us-east-1\n",
			gone: []string{"AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY"},
			kept: []string{"[default]", "region = us-east-1"},
		},
		{
			name: "a token response on one line",
			in:   `{"access_token":"abc123opaque","token_type":"bearer"}`,
			gone: []string{"abc123opaque"},
			kept: []string{`{"access_token": [redacted]`},
		},
		{
			name: "a one-line JSON with the credential second",
			in:   `{"name":"x","token":"abc123opaque"}`,
			gone: []string{"abc123opaque"},
			kept: []string{`{"name":"x","token": [redacted]`},
		},
		{
			name: "an environment dump",
			in: "export AWS_SESSION_TOKEN=FwoGZXIvYXdzEXAMPLE\n" +
				"KUBECONFIG=/home/u/.kube/config\n" +
				"GITHUB_TOKEN=ghp_0123456789abcdefghijklmnopqrstuvwxyz\n",
			gone: []string{"FwoGZXIvYXdzEXAMPLE", "ghp_0123456789abcdefghijklmnopqrstuvwxyz"},
			kept: []string{"export AWS_SESSION_TOKEN=[redacted]", "KUBECONFIG=/home/u/.kube/config"},
		},
		{
			name: "a PEM key opening an env line, the ordering pinned",
			in: "PRIVATE_KEY=-----BEGIN PRIVATE KEY-----\nMIIEvQIBADAN\n-----END PRIVATE KEY-----\n" +
				"NEXT=kept\n",
			gone: []string{"MIIEvQIBADAN", "BEGIN"},
			kept: []string{"PRIVATE_KEY=[redacted]", "NEXT=kept"},
		},
		{
			name: "the names with no anchor are prose",
			in: "oauth2: cannot fetch token: 401 Unauthorized\n" +
				"token expired; the password must be 8 characters\ntokens: 5\ntoken_count=3\n",
			kept: []string{
				"oauth2: cannot fetch token: 401 Unauthorized",
				"token expired; the password must be 8 characters", "tokens: 5", "token_count=3",
			},
		},
		{
			name: "a name with no value and nothing deeper",
			in:   "token:\nname: prod\n",
			kept: []string{"token:\nname: prod"},
		},
		{
			name: "an unquoted name after a comma is prose, not a one-line JSON's field",
			in:   "auth failed, token: expired at 10:07",
			kept: []string{"auth failed, token: expired at 10:07"},
		},
		{
			name: "a Slack token and a Google API key in a log line",
			in: "webhook rejected xoxb-1234567890-abcdefghij\n" +
				"maps refused AIzaSyA0123456789012345678901234567890123 and " +
				"AIzaSyB01234567890123456789012345678901234567 alike",
			gone: []string{"xoxb-1234567890-abcdefghij", "AIzaSyA0123456789012345678901234567890123", "AIzaSyB01234567890123456789012345678901234567"},
			kept: []string{"webhook rejected", "maps refused", "alike"},
		},
		{
			name: "a PEM block over its own lines",
			in:   "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\nok",
			gone: []string{"MIIEowIBAAKCAQEA"},
			kept: []string{"-----BEGIN RSA PRIVATE KEY-----[redacted]-----END RSA PRIVATE KEY-----", "ok"},
		},
		{
			name: "a PEM block the capture cut before its END",
			in:   "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIABC",
			gone: []string{"MHcCAQEEIABC"},
			kept: []string{"-----BEGIN EC PRIVATE KEY-----[redacted]"},
		},
		{
			name: "a certificate is the public half and stays",
			in:   "-----BEGIN CERTIFICATE-----\nMIICmyPublicCert\n-----END CERTIFICATE-----",
			kept: []string{"-----BEGIN CERTIFICATE-----", "MIICmyPublicCert", "-----END CERTIFICATE-----"},
		},
		{
			name: "a PEM key inside one JSON line, the newlines two characters",
			in:   `{"tls.key": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADAN\n-----END PRIVATE KEY-----\n"}`,
			gone: []string{"MIIEvQIBADAN"},
			kept: []string{"-----BEGIN PRIVATE KEY-----[redacted]-----END PRIVATE KEY-----"},
		},
		{
			name: "a JWT in free text",
			in:   "id_token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.c2lnbmF0dXJl failed verification",
			gone: []string{"eyJzdWIiOiJhbGljZSJ9", "c2lnbmF0dXJl"},
			kept: []string{"id_token", "failed verification"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := safe.Safe(errors.New(c.in))

			for _, s := range c.gone {
				assert.NotContains(t, got, s)
			}
			for _, s := range c.kept {
				assert.Contains(t, got, s)
			}
		})
	}
}

// A hostname is dots and dashes too, and a bare path is not a URL; nothing here is a
// credential shape, so an ordinary error keeps every character it had.
func TestSafeLeavesAnOrdinaryErrorAlone(t *testing.T) {
	in := "kubestore: open cluster cache 7: database is locked (svc.kube-system.example)"

	assert.Equal(t, in, safe.Safe(errors.New(in)))
}

func TestSafeBoundsALongMessage(t *testing.T) {
	got := safe.Safe(fmt.Errorf("readyz: %s", strings.Repeat("x", 8*1024)))

	assert.LessOrEqual(t, len(got), safe.MaxLen+len("…"))
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.Contains(t, got, "readyz:")
}

func TestSafeRendersANilErrorAsEmpty(t *testing.T) {
	assert.Empty(t, safe.Safe(nil))
}

// A registered value goes wherever it appears — prose, a URL's query, a JSON body
// — whatever its shape, since the vendors' keys share no prefix. Registered under
// Cleanup, so no case sees another's values.
func TestSafeBlanksARegisteredValueWhereverItAppears(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	safe.AddSecret("gsk_LIVEKEY0123456789")
	safe.AddSecret("AIzaSyLIVEKEY0123456789")

	cases := map[string]string{
		"in a sentence": "groq: refused key gsk_LIVEKEY0123456789 for this model",
		"in a URL":      "Get https://api.example.com/v1/models?key=AIzaSyLIVEKEY0123456789: 401",
		"in JSON":       `{"error":{"message":"bad key: gsk_LIVEKEY0123456789"}}`,
		"both at once":  "gsk_LIVEKEY0123456789 then AIzaSyLIVEKEY0123456789",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := safe.String(in)

			assert.NotContains(t, got, "LIVEKEY")
			assert.Contains(t, got, "[redacted]")
		})
	}
	assert.Equal(t, "[redacted] then [redacted]", safe.String("gsk_LIVEKEY0123456789 then AIzaSyLIVEKEY0123456789"))
}

// No vendor's key is under sixteen bytes, and a short value registered would blank
// its letters out of every log line.
func TestSafeRegistersNoValueUnderSixteenBytes(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	safe.AddSecret("")
	safe.AddSecret("fifteen-bytes15")
	safe.AddSecret("sixteen-bytes016")

	assert.Equal(t, "fifteen-bytes15 stays", safe.String("fifteen-bytes15 stays"))
	assert.Equal(t, "[redacted] goes", safe.String("sixteen-bytes016 goes"))
}

// Matches are found in the original text and merged, so a short key that is a
// substring of a long one leaves no tail of the long one in the clear, whichever
// was registered first.
func TestSafeBlanksALongKeyWholeWhenAShortOneIsInsideIt(t *testing.T) {
	short, long := "sk-short-0123456789", "sk-short-0123456789-and-a-longer-tail"
	for name, order := range map[string][]string{"short first": {short, long}, "long first": {long, short}} {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(safe.ResetSecrets)
			for _, v := range order {
				safe.AddSecret(v)
			}

			assert.Equal(t, "key [redacted] refused", safe.String("key "+long+" refused"))
			assert.Equal(t, "key [redacted] refused", safe.String("key "+short+" refused"))
		})
	}
}

// What a `cat` of a mixed directory produces: every rule at once, in one text. The one
// ordering that matters is pinned here — the PEM key opening an env line, whose BEGIN the
// field rule would take if it ran first, leaving the body below matching nothing.
func TestRedactComposesTheRules(t *testing.T) {
	in := strings.Join([]string{
		"users:",
		"- name: prod",
		"  user:",
		"    token: |",
		"      eyJhLmIuYw",
		"    client-certificate-data: LS0tQ0VSVA==",
		"PRIVATE_KEY=-----BEGIN PRIVATE KEY-----",
		"MIIEvQIBADAN",
		"-----END PRIVATE KEY-----",
		"NEXT=kept",
		"tls.key: |",
		"  -----BEGIN RSA PRIVATE KEY-----",
		"  MIIEowIBAAKCAQEA",
		"  -----END RSA PRIVATE KEY-----",
		"default password hunter2opaque",
		"kubectl --token=t0psecret --server=https://k8s.example.com",
		`Get "https://svc:pw@api.example.com/readyz?token=SEKRIT": refused`,
	}, "\n")

	got := safe.Redact(in)

	for _, gone := range []string{
		"eyJhLmIuYw", "MIIEvQIBADAN", "MIIEowIBAAKCAQEA", "hunter2opaque", "t0psecret",
		"SEKRIT", "svc:pw",
	} {
		assert.NotContains(t, got, gone)
	}
	for _, kept := range []string{
		"name: prod", "token: [redacted]", "LS0tQ0VSVA==", "PRIVATE_KEY=[redacted]",
		"NEXT=kept", "tls.key: |", "-----BEGIN RSA PRIVATE KEY-----[redacted]-----END RSA PRIVATE KEY-----",
		"default password [redacted]", "--token=[redacted] --server=https://k8s.example.com",
		"api.example.com/readyz", "refused",
	} {
		assert.Contains(t, got, kept)
	}
}

// Redact is String without the cap: a command's output is redacted whole and
// cut by its own budget afterwards, so a token across that cut is whole when
// the pattern sees it.
func TestRedactKeepsTheLength(t *testing.T) {
	head := strings.Repeat("a", 5*1024)
	tail := strings.Repeat("b", 5*1024)
	in := head + "\nAuthorization: Bearer abc.def.ghi\n" + tail
	out := safe.Redact(in)
	assert.Equal(t, head+"\nAuthorization: [redacted]\n"+tail, out)
	assert.Greater(t, len(out), safe.MaxLen)
}

// The hand-written cases pin the shapes we thought of; this pins the ones we did not. Every
// review of these rules has found the same class of bug — a value whose end is not on the
// line its name is on — so the generator is over containers (where a value may continue) and
// over placements (what surrounds it), and the property is that no piece of the planted value
// survives.
//
// The planted value matches no form rule: only the container's own name can give it away, so
// a pass proves the structural rules read the container rather than that some regex
// recognised the value.
const (
	plantedHead = "PLANTEDheadZZ0value1"
	plantedTail = "PLANTEDtailZZ2value3"
)

// container builds one credential-carrying shape around a field name, and names the pieces of
// the planted value that must not survive. A shape that cannot carry a continuation plants the
// head alone.
type container struct {
	name  string
	build func(field string) (text string, planted []string)
}

var oneLine = []container{
	{name: "inline", build: func(f string) (string, []string) {
		return f + ": " + plantedHead, []string{plantedHead}
	}},
	{name: "inline quoted", build: func(f string) (string, []string) {
		return f + `: "` + plantedHead + `"`, []string{plantedHead}
	}},
	{name: "inline single-quoted", build: func(f string) (string, []string) {
		return f + ": '" + plantedHead + "'", []string{plantedHead}
	}},
	{name: "block scalar", build: func(f string) (string, []string) {
		return f + ": |\n  " + plantedHead + "\n  " + plantedTail, []string{plantedHead, plantedTail}
	}},
	{name: "folded scalar with a header comment", build: func(f string) (string, []string) {
		return f + ": >- # credential\n  " + plantedHead, []string{plantedHead}
	}},
	{name: "flow scalar wrapped deeper", build: func(f string) (string, []string) {
		return f + `: "` + plantedHead + "\n  " + plantedTail + `"`, []string{plantedHead, plantedTail}
	}},
	{name: "json one line", build: func(f string) (string, []string) {
		return `{"` + f + `":"` + plantedHead + `"}`, []string{plantedHead}
	}},
	{name: "json one line, credential second", build: func(f string) (string, []string) {
		return `{"kind":"x","` + f + `":"` + plantedHead + `"}`, []string{plantedHead}
	}},
	{name: "sequence marker", build: func(f string) (string, []string) {
		return "- " + f + ": " + plantedHead, []string{plantedHead}
	}},
	{name: "nested map under the name", build: func(f string) (string, []string) {
		return f + ":\n  file: " + plantedHead, []string{plantedHead}
	}},
	{name: "export assignment", build: func(f string) (string, []string) {
		return "export " + strings.ToUpper(strings.ReplaceAll(f, "-", "_")) + "=" + plantedHead,
			[]string{plantedHead}
	}},
}

// The flag rules take a shorter name list than the field rules, so their containers are
// generated over it alone.
var flagLine = []container{
	{name: "flag, = form", build: func(f string) (string, []string) {
		return "kubectl --" + f + "=" + plantedHead, []string{plantedHead}
	}},
	{name: "flag, = form quoted", build: func(f string) (string, []string) {
		return `kubectl --` + f + `="` + plantedHead + `"`, []string{plantedHead}
	}},
	{name: "flag, blank form", build: func(f string) (string, []string) {
		return "kubectl --" + f + " " + plantedHead, []string{plantedHead}
	}},
}

var netrcLine = []container{
	{name: "netrc, one line", build: func(string) (string, []string) {
		return "machine h login bob password " + plantedHead, []string{plantedHead}
	}},
	{name: "netrc, value on the next line", build: func(string) (string, []string) {
		return "machine h login bob password\n  " + plantedHead, []string{plantedHead}
	}},
	{name: "netrc, quoted value", build: func(string) (string, []string) {
		return `machine h login bob password "` + plantedHead + " " + plantedTail + `"`,
			[]string{plantedHead, plantedTail}
	}},
	{name: "netrc, quoted value with an escaped quote", build: func(string) (string, []string) {
		return `machine h login bob password "` + plantedHead + `\" ` + plantedTail + `"`,
			[]string{plantedHead, plantedTail}
	}},
	{name: "netrc, quoted login before the value", build: func(string) (string, []string) {
		return `machine h login "first last" password ` + plantedHead, []string{plantedHead}
	}},
}

// placement wraps a container in what a real capture puts around it. `kept` is a line that
// must survive, so a placement can never pass by redacting everything.
var placements = []struct {
	name string
	wrap func(body string) (text, kept string)
}{
	{name: "alone", wrap: func(b string) (string, string) { return b, "" }},
	{name: "above a sibling", wrap: func(b string) (string, string) {
		return b + "\nnextkept: yes", "nextkept: yes"
	}},
	{name: "below a sibling", wrap: func(b string) (string, string) {
		return "firstkept: yes\n" + b, "firstkept: yes"
	}},
	{name: "between siblings", wrap: func(b string) (string, string) {
		return "firstkept: yes\n" + b + "\nnextkept: yes", "nextkept: yes"
	}},
	{name: "indented under a parent", wrap: func(b string) (string, string) {
		return "parent:\n" + indentBy(b, 2) + "\nnextkept: yes", "nextkept: yes"
	}},
	{name: "indented deeper under a parent", wrap: func(b string) (string, string) {
		return "parent:\n  child:\n" + indentBy(b, 4) + "\nnextkept: yes", "nextkept: yes"
	}},
	{name: "after prose", wrap: func(b string) (string, string) {
		return "reading the file failed\n" + b, "reading the file failed"
	}},
	{name: "the capture ended right after", wrap: func(b string) (string, string) { return b, "" }},
}

// indentBy shifts every line of a block, as a nested mapping does.
func indentBy(block string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func TestRedactTakesAPlantedValueWhateverContainsIt(t *testing.T) {
	groups := []struct {
		containers []container
		fields     []string
	}{
		{oneLine, []string{
			"token", "password", "client-key-data", "private_key", "client_secret",
			"aws_secret_access_key", "aws_session_token", "api-key", "access_token",
		}},
		{flagLine, []string{"token", "password", "client-secret", "api-key"}},
		{netrcLine, []string{"password"}},
	}
	for _, g := range groups {
		for _, c := range g.containers {
			for _, f := range g.fields {
				for _, p := range placements {
					body, planted := c.build(f)
					in, kept := p.wrap(body)
					name := fmt.Sprintf("%s/%s/%s", c.name, f, p.name)
					t.Run(name, func(t *testing.T) {
						got := safe.Redact(in)

						for _, secret := range planted {
							assert.NotContains(t, got, secret, "input:\n%s\noutput:\n%s", in, got)
						}
						if kept != "" {
							assert.Contains(t, got, kept, "input:\n%s\noutput:\n%s", in, got)
						}
					})
				}
			}
		}
	}
}

// HasSecret is asked of a note the model wants to keep, so both sides matter: a
// credential in any shape it knows is found, and what a note ordinarily holds — a
// dashboard link, a sentence about a password — is not.
func TestHasSecretFindsACredentialAndLeavesANoteAlone(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	safe.AddSecret("gsk_LIVEKEY0123456789")

	secret := map[string]string{
		"a registered value": "the key is gsk_LIVEKEY0123456789",
		"a PEM private key":  "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----",
		"URL userinfo":       "clone https://alice:hunter2hunter2@git.example.com/repo",
		"a credential field": "token: 0123456789abcdef",
		"a JSON field":       `{"user":"a","password":"hunter2"}`,
		"a credential flag":  "kubectl --token=0123456789abcdef get pods",
		"an auth header":     "Authorization: Basic YWxhZGRpbjpvcGVuc2VzYW1l",
		"a bearer token":     "send bearer abc.def.ghi with it",
		"a JWT":              "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"a provider key":     "sk-ant-0123456789abcdefghij",
		"an AWS key id":      "AKIAIOSFODNN7EXAMPLE",
		"a GitHub token":     "ghp_0123456789abcdefghijklmnopqrstuvwxyz",
	}
	for name, in := range secret {
		t.Run(name, func(t *testing.T) {
			assert.True(t, safe.HasSecret(in))
		})
	}

	note := map[string]string{
		"a dashboard link":        "https://grafana.internal/d/abc?orgId=1&var-ns=payments",
		"a line opening password": "password rotation is monthly, owned by the platform team",
		"a pointer to a secret":   "the db password lives in vault at secret/payments/db",
		"a YAML null":             "token:",
		"plain prose":             "payments is the namespace that pages on-call",
	}
	for name, in := range note {
		t.Run(name, func(t *testing.T) {
			assert.False(t, safe.HasSecret(in))
		})
	}
}

// Kubernetes and most CRDs spell keys in camelCase, so a credential's name is read with
// or without the separator inside it.
func TestTheFieldRuleReadsCamelCase(t *testing.T) {
	in := "clientSecret: abc\naccessToken: def\napiKey: ghi\nsecretAccessKey: jkl\nprivateKey: mno"
	want := "clientSecret: [redacted]\naccessToken: [redacted]\napiKey: [redacted]\nsecretAccessKey: [redacted]\nprivateKey: [redacted]"
	assert.Equal(t, want, safe.Redact(in))
}

// A forgotten value is no longer blanked; one added twice and forgotten once
// still is, since its other holder still holds it.
func TestAForgottenSecretIsNotBlanked(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	safe.AddSecret("rotating-token-0123456789")
	safe.AddSecret("shared-token-0123456789")
	safe.AddSecret("shared-token-0123456789")

	safe.ForgetSecret("rotating-token-0123456789")
	safe.ForgetSecret("shared-token-0123456789")
	safe.ForgetSecret("never-added-0123456789")

	assert.Equal(t, "token rotating-token-0123456789", safe.String("token rotating-token-0123456789"))
	assert.Equal(t, "token "+safe.Redacted, safe.String("token shared-token-0123456789"))
}

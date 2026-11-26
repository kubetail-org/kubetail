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

// Package safe renders an error for a log line or a stored message, and redacts a
// command's output before the model reads it.
//
// Three callers, for the three places text ends up. Logs go through logging's handler,
// which renders every record whoever wrote it, so an ordinary slog call needs nothing here.
// A message that is persisted instead — a condition served to the UI — has no such sink, so
// it is rendered where it is recorded. A bash result goes through Redact alone, cut by its
// caller's own budget afterwards, and a KubeQuery cell holding JSON through RedactJSON,
// which runs Redact over each of its strings.
//
// Redaction is hygiene against accidental spillage, never a boundary. The rules cover the
// shapes real tools print — an echoed header, a kubeconfig, an env dump, ps, a .netrc —
// and a command can always re-encode its output past any rule (`… | base64`), which is why
// the bash tool's boundary is the user's approval of the command, not this package. The
// residual that follows is stated in the security model's redaction row: a printed form
// interrupted before its closing delimiter — by the capture limit, a kill, or a value's
// own blanks or newlines — can leak past its first token. A new instance of that class
// belongs to the stated residual; it is not a defect in a rule.
package safe

import (
	"regexp"
	"slices"
	"strings"
	"sync"
)

// MaxLen bounds a rendered error. A log line is read once, by a human, so the cap only has
// to stop a kilobyte-scale response body (a verbose /readyz) from landing whole — which is
// why it is not cluster's much tighter cap on a message re-read on every watch frame.
const MaxLen = 2048

// Redacted is the mark put in place of what is removed.
const Redacted = "[redacted]"

// quoted is a double-quoted word with backslash escapes, as curl reads a .netrc's. A rule
// that reads quoted tokens must take one whole or not at all: a partial match deletes the
// head and leaves the tail of the value in the clear.
const quoted = `"(?:[^"\\]|\\.)*"`

// credentialNames are the names a credential goes by in the formats Redact reads: a
// kubeconfig's, a cloud credentials file's, a service-account JSON's, a token response's,
// an env dump's, a CRD's. The field and flag rules are built from them, and RedactJSON
// reads a key against them.
var credentialNames = []string{
	"client-key-data", "id-token", "refresh-token", "access-token", "token", "password",
	"private-key", "client-secret", "aws-secret-access-key", "aws-session-token",
	"secret-access-key", "api-key",
}

// credentialPattern matches any of credentialNames, a separator inside one written as
// `-`, `_` or nothing: formats spell the same name `client_secret`, `client-secret` and
// `clientSecret`.
var credentialPattern = func() string {
	alts := make([]string, len(credentialNames))
	for i, name := range credentialNames {
		alts[i] = strings.ReplaceAll(regexp.QuoteMeta(name), "-", "[-_]?")
	}
	return strings.Join(alts, "|")
}()

// What a credential looks like inside an error, rather than a keyword list — which is how
// the next format slips past. The order they run in is Redact's, and the rule for it —
// containers before token rules — is stated there.
var (
	// A header echoed back by a server, value to the end of its line. The two api-key
	// spellings are the model providers': the key is the header, not a bearer token.
	headerRE = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|x-api-key|api-key)\s*[:=][^\n]*`)
	// A URL, to the first character that cannot be in one. Its query and userinfo are the
	// two parts that carry credentials; the host and path are the diagnostic.
	urlRE = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s"'<>\\]+`)
	// A bearer token in free text, and a JWT wherever it appears. A JWT is identified by
	// its header segment, the base64 of `{"` — a length guess over dot-separated words
	// would also match a hostname.
	bearerRE = regexp.MustCompile(`(?i)\bbearer\s+[^\s"']+`)
	jwtRE    = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]*\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]*`)
	// A private key in PEM, whole, wherever its lines are: its own, one JSON line's with the
	// two characters `\n` between them, a YAML block's. The two guards stay so the reader sees
	// what was there; the body goes, and with it whatever separated the three. A block the
	// capture cut before its END is taken to the end of the text, or the head of a key leaves.
	// A CERTIFICATE block is the public half and authenticates nothing, so it stays.
	pemRE = regexp.MustCompile(`(?s)-----BEGIN ([A-Z ]*PRIVATE KEY)-----.*?(-----END [A-Z ]*PRIVATE KEY-----|\z)`)
	// A credential field by the name its format gives it: a kubeconfig's, a cloud credentials
	// file's, a service-account JSON's, a token response's, an environment dump's. The name
	// opens the line — after a YAML sequence's markers, and an `export` may precede it — or
	// follows a `{` or `,` on a one-line JSON, quoted there as JSON writes every name: an
	// unquoted name after a comma is prose (`auth failed, token: expired`) or another
	// container's contents — a quoted .netrc password can hold `,token:` — and stays.
	// `token` mid-sentence is prose and stays. Quotes around the name are kept as written;
	// hyphen and underscore are one character, since formats spell the same name both ways.
	// `[ \t]` and not `\s` around the delimiter, so the match never reads past the line.
	fieldRE = regexp.MustCompile(`(?i)(^[ \t]*(?:-[ \t]+)*(?:export[ \t]+)?["']?|[{,][ \t]*")((?:` + credentialPattern + `)["']?)[ \t]*([:=])[ \t]*(.*)`)
	// A credential on a command line as a shell wrote it — a script, a verbose plugin echoing
	// what it ran, ps printing argv flattened. The value is one blank-free run: a token is,
	// and where a quoted value held a blank the first word goes and the rest stays, which is
	// the stated residual — flattened argv has lost the quoting anyway, so nothing in the text
	// says where the argument ended. After a blank the run must not open the next flag, so a
	// bare `--token` before `--server` takes nothing. headerRE takes what is left of an
	// `--api-key=…` line afterwards — the one name the two rules share — so that flag reads
	// `--api-key: [redacted]` and the line's tail goes with it.
	flagEqRE = regexp.MustCompile(`(?i)--(` + credentialPattern + `)=\S+`)
	flagSpRE = regexp.MustCompile(`(?i)--(` + credentialPattern + `)([ \t]+)[^\s-]\S*`)
	// A .netrc password. The file's tokens are separated by any whitespace, newlines included,
	// come in any order (`machine h password p login u`), any value may be quoted with
	// backslash escapes as curl reads it, and `default` stands where a machine would. So the
	// rule is anchored to what a .netrc puts before the word — a line start, `default`, or
	// another keyword and its value — which is what keeps `password must be 8 characters`
	// whole mid-sentence. A line that opens with the word loses the word after it, on that
	// line or the next; there is no telling that line from a .netrc's. The keyword arm carries
	// the same price mid-sentence — `your account and password reset` reads as a .netrc's
	// order — and in both the price is one word. `password: x` is not matched, so a field and
	// a .netrc line never both apply.
	netrcRE = regexp.MustCompile(`(?im)(^[ \t]*|\bdefault\s+|\b(?:machine|login|account)\s+(?:` + quoted + `|\S+)\s+)password\s+(` + quoted + `|\S+)`)
	// A model provider's key wherever it appears: the sk- prefix both use, then a long
	// token. The length is what keeps it off ordinary prose.
	apiKeyRE = regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}`)
	// An AWS access key id. The 40-character secret beside it has no form of its own;
	// fieldRE takes it by name.
	awsKeyRE = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)
	// Tokens whose issuer gave them a prefix: GitHub, GitLab, Slack, a Google API key. The
	// set is what a cluster command plausibly prints — an env dump, a pulled Secret, a CI
	// config — and no wider. Each length is open-ended: a fixed count would leave the tail
	// of a longer token in the clear.
	prefixedRE = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|glpat-[A-Za-z0-9_\-]{20,}|xox[abprs]-[A-Za-z0-9\-]{10,}|AIza[0-9A-Za-z_\-]{35,})`)
)

// MinSecretLen is the shortest value AddSecret registers. No vendor's key is under
// it, and a short value would blank its letters out of every line.
const MinSecretLen = 16

// secrets is every value AddSecret registered, read on every render.
var (
	secretsMu sync.RWMutex
	secrets   []string
)

// AddSecret registers a value to blank wherever it appears in rendered text:
// a key the config read, before anything is logged, or a credential a service
// holds, until it calls ForgetSecret. A value under MinSecretLen registers
// nothing.
func AddSecret(value string) {
	if len(value) < MinSecretLen {
		return
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	secrets = append(secrets, value)
}

// ForgetSecret drops one registration of value, so a credential that rotates
// does not grow the list. A value registered twice stays blanked until each
// registration is forgotten.
func ForgetSecret(value string) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	i := slices.Index(secrets, value)
	if i < 0 {
		return
	}
	// A new slice, since blankSecrets reads the old one outside the lock.
	secrets = slices.Concat(secrets[:i], secrets[i+1:])
}

// ResetSecrets forgets every registered value. For tests.
func ResetSecrets() {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	secrets = nil
}

// blankSecrets replaces every occurrence of every registered value with one
// marker. Matches are found in the original text and merged where they overlap
// or touch: replacing one value after another would leave the tail of a long key
// in the clear once a shorter key inside it had gone.
func blankSecrets(s string) string {
	secretsMu.RLock()
	values := secrets
	secretsMu.RUnlock()
	type span struct{ start, end int }
	var spans []span
	for _, v := range values {
		for from := 0; ; {
			i := strings.Index(s[from:], v)
			if i < 0 {
				break
			}
			spans = append(spans, span{from + i, from + i + len(v)})
			from += i + 1
		}
	}
	if len(spans) == 0 {
		return s
	}
	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })
	var b strings.Builder
	at := 0
	for i := 0; i < len(spans); {
		merged := spans[i]
		for i++; i < len(spans) && spans[i].start <= merged.end; i++ {
			merged.end = max(merged.end, spans[i].end)
		}
		b.WriteString(s[at:merged.start])
		b.WriteString(Redacted)
		at = merged.end
	}
	b.WriteString(s[at:])
	return b.String()
}

// Safe renders err bounded and with the credential-carrying shapes removed.
func Safe(err error) string {
	if err == nil {
		return ""
	}
	return String(err.Error())
}

// String renders text the same way, for a value that reaches a log without ever having
// been an error — a log message, an attribute a dependency set.
func String(s string) string {
	s = Redact(s)
	// Last, so no rule ever reads a string a cut left half a token in.
	if len(s) > MaxLen {
		return s[:MaxLen] + "…"
	}
	return s
}

// Redact removes the credential-carrying shapes from text of any length, for a
// caller with a cut of its own to make afterwards.
func Redact(s string) string {
	// Registered values first, so no shape rule ever reads one.
	s = blankSecrets(s)
	// Every container rule — a match bounded by delimiters the input wrote: PEM's guards, a
	// URL's userinfo closed by its @, a field's lines, netrc's quoted value — runs before the
	// token rules, which rewrite bare spans. A rule matching inside a container takes its
	// closing delimiter with it, and the half-matched container then leaks its tail. Among
	// the containers, PEM before the field rule: a key that opens an env line
	// (`PRIVATE_KEY=-----BEGIN…`) has its BEGIN taken with that line, and the body below
	// would then match nothing; and the URL before the field and flag rules, whose shapes a
	// userinfo can hold.
	s = pemRE.ReplaceAllStringFunc(s, redactPEM)
	s = urlRE.ReplaceAllStringFunc(s, redactURL)
	s = redactFields(s)
	s = netrcRE.ReplaceAllString(s, "${1}password "+Redacted)
	s = flagEqRE.ReplaceAllString(s, "--$1="+Redacted)
	s = flagSpRE.ReplaceAllString(s, "--$1$2"+Redacted)
	// The captured name, never a re-split of the match: the delimiter may be either, and a
	// value carries delimiters of its own.
	s = headerRE.ReplaceAllString(s, "$1: "+Redacted)
	s = bearerRE.ReplaceAllString(s, "Bearer "+Redacted)
	s = jwtRE.ReplaceAllString(s, Redacted)
	s = apiKeyRE.ReplaceAllString(s, Redacted)
	s = awsKeyRE.ReplaceAllString(s, Redacted)
	return prefixedRE.ReplaceAllString(s, Redacted)
}

// HasSecret reports whether s holds a credential: a registered value, or a shape
// Redact removes for being one. It leaves out two of Redact's rules, which suit an
// error and not a note: a URL's query, where a dashboard link keeps its view, and
// the .netrc rule, which reads a line opening with "password" as a credential.
func HasSecret(s string) bool {
	if blankSecrets(s) != s || pemRE.MatchString(s) || redactFields(s) != s {
		return true
	}
	for _, u := range urlRE.FindAllString(s, -1) {
		head, _, _ := strings.Cut(u, "?")
		if redactURL(head) != head {
			return true
		}
	}
	for _, re := range []*regexp.Regexp{flagEqRE, flagSpRE, headerRE, bearerRE, jwtRE, apiKeyRE, awsKeyRE, prefixedRE} {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// redactFields rewrites every line fieldRE matches to the name, its delimiter and the
// marker, and takes the value's deeper lines with it: in a YAML mapping a scalar's
// continuation — a block's body, a wrapped quoted string — is indented deeper than its key.
// A block scalar needs no rule of its own: its header (`|`, `>-`, `|2-`) is the value on the
// name's line and its body is the deeper lines. The rest of a matched line goes with the
// value, so a one-line JSON loses the fields after the credential, which is the cheap side;
// a listed name over a nested map loses the map for the same reason. A name with nothing
// after its delimiter and nothing deeper is YAML's null and stays.
func redactFields(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		m := fieldRE.FindStringSubmatchIndex(line)
		if m == nil {
			out = append(out, line)
			continue
		}
		// A CRLF leaves its \r on the value; it is blank here.
		value := strings.TrimRight(line[m[8]:m[9]], " \t\r")
		// The key's depth is the column of its first written character — the opening
		// quote when it has one, which the anchor holds — not the line's first blank: a
		// sequence marker (`- token: x`) indents the mapping it opens, so the item's
		// other fields sit at the key's depth and only deeper lines are the value's.
		at := m[4]
		if at > 0 && (line[at-1] == '"' || line[at-1] == '\'') {
			at--
		}
		j := i
		for j+1 < len(lines) && (strings.TrimSpace(lines[j+1]) == "" || indent(lines[j+1]) > at) {
			j++
		}
		if j == i && value == "" {
			out = append(out, line)
			continue
		}
		i = j
		delim := line[m[6]:m[7]]
		if delim == ":" {
			delim = ": "
		}
		// m[5] is the end of the name as written, quotes included.
		out = append(out, line[:m[5]]+delim+Redacted)
	}
	return strings.Join(out, "\n")
}

// indent is the width of a line's leading blanks.
func indent(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }

// redactPEM keeps a block's two guards and drops its body, so the result is one line
// whatever the block spanned.
func redactPEM(block string) string {
	g := pemRE.FindStringSubmatch(block)
	return "-----BEGIN " + g[1] + "-----" + Redacted + g[2]
}

// redactURL keeps the scheme, host and path — what says which server refused — and drops
// the query and the userinfo.
func redactURL(u string) string {
	head, _, hasQuery := strings.Cut(u, "?")
	if i := strings.Index(head, "//"); i >= 0 {
		authority, rest, _ := strings.Cut(head[i+2:], "/")
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			head = head[:i+2] + Redacted + "@" + authority[at+1:]
			if rest != "" {
				head += "/" + rest
			}
		}
	}
	if hasQuery {
		return head + "?" + Redacted
	}
	return head
}

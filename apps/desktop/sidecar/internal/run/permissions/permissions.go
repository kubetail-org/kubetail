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

// Package permissions decides, for one classified request a sandboxed command
// sends, whatever it reaches, whether it runs, asks or is refused. A classifier
// per target reduces a request to one Action; the boundary, the account's
// mode and the rules decide it, and none of them branches on the target.
// Authorization fails closed: a deny wins over an ask, an ask over an allow,
// and an action nothing allows asks. A leaf: the proxies that enforce it
// import it, and it imports nothing of ours.
package permissions

import (
	"slices"
	"strings"
)

// AccessLevel is how much an action can do, stored and served by name. An
// action has exactly one: the first of Reach, Destructive, AccessControl,
// DataRead, Write and Read that applies.
type AccessLevel string

const (
	Read          AccessLevel = "read"           // metadata, and what changes nothing
	DataRead      AccessLevel = "data-read"      // Secret data, an object's contents, key material
	Write         AccessLevel = "write"          // a change that can be undone
	AccessControl AccessLevel = "access-control" // who may do what, and what sees every request
	Destructive   AccessLevel = "destructive"    // loss that cannot be undone, or a broad outage
	Reach         AccessLevel = "reach"          // exec, tunnels, credential minting
)

// Levels is every access level.
var Levels = []AccessLevel{Read, DataRead, Write, AccessControl, Destructive, Reach}

// LevelNamed is the level stored as name, false for a name no level has.
func LevelNamed(name string) (AccessLevel, bool) {
	l := AccessLevel(name)
	return l, slices.Contains(Levels, l)
}

// covers is what each level's deny or ask rule reaches beyond itself: a
// destructive change and an access control change are writes.
var covers = map[AccessLevel][]AccessLevel{Write: {Destructive, AccessControl}}

// Covers is whether a deny or ask rule of level l matches an action of level o.
func (l AccessLevel) Covers(o AccessLevel) bool {
	return l == o || slices.Contains(covers[l], o)
}

// Principal is who sent an action: the kind of session its run holds.
type Principal string

const (
	Chat     Principal = "chat"
	Subagent Principal = "subagent"
	Monitor  Principal = "monitor"
	// Console is the Sandbox console's: a command the user typed in Settings.
	Console Principal = "console"
)

// Principals is every principal.
var Principals = []Principal{Chat, Subagent, Monitor, Console}

// Target is what an action reaches. The zero value is no target.
type Target string

const (
	Kubernetes Target = "kubernetes"
	AWS        Target = "aws"
	Azure      Target = "azure"
	GCP        Target = "gcp"
	GitHub     Target = "github"
)

// Targets is every target.
var Targets = []Target{Kubernetes, AWS, Azure, GCP, GitHub}

// TargetNamed is the target named name, false for none.
func TargetNamed(name string) (Target, bool) {
	t := Target(name)
	return t, slices.Contains(Targets, t)
}

// targetWords is how a line names a target before its account. A
// Kubernetes account is a cluster, named alone.
var targetWords = map[Target]string{AWS: "AWS", Azure: "Azure", GCP: "Google Cloud", GitHub: "GitHub"}

// Action is one classified request, as its target's classifier read it. It is
// stored with the approval that records it.
type Action struct {
	Principal Principal `json:"principal"` // who sent it; the proxy stamps it
	Target    Target    `json:"target"`
	// Scope is a path of /-separated segments, the first the account: a
	// cluster's record id, a cloud account. "" is an account the classifier
	// could not trust, which asks each time. No segment holds /.
	Scope       string      `json:"scope"`
	AccountName string      `json:"accountName"` // the account in the user's words; never matched
	Service     string      `json:"service"`     // the API group ("" for core), the cloud service
	Operation   string      `json:"operation"`   // the verb or the operation's name
	Resource    string      `json:"resource"`    // the resource's type, a path of segments
	Name        string      `json:"name"`        // the object's name; never matched
	AccessLevel AccessLevel `json:"accessLevel"`
	// Confined is the classifier's word that the scope bounds what an
	// access control action can grant, so a rule may allow it.
	Confined bool `json:"confined"`
	// Once is the classifier's word that the action asks each time: an
	// operation no table of its names.
	Once bool `json:"once"`
	// Spans is the classifier's word that the action reaches every scope below
	// its own, as a list across namespaces does: a deny or ask rule for any of
	// them applies to it, and only an allow reaching them all allows it.
	Spans   bool   `json:"spans"`
	Summary string `json:"summary"` // one line, written by the classifier
	// DryRun is a request for a dry run. No rule names one, so a rule written
	// from it would allow the real write.
	DryRun bool `json:"dryRun"`
}

// Account is act's account, its scope's first segment: "" for none.
func (act Action) Account() string {
	account, _, _ := strings.Cut(act.Scope, "/")
	return account
}

// Effect is what a verdict or a rule does to an action.
type Effect string

const (
	Allow Effect = "allow"
	Ask   Effect = "ask"
	Deny  Effect = "deny"
)

// Mode is an account's word on what runs unasked.
type Mode string

const (
	ModeReadOnly Mode = "read-only"
	ModeAsk      Mode = "ask"
	ModeAuto     Mode = "auto"
)

// Modes is every mode.
var Modes = []Mode{ModeReadOnly, ModeAsk, ModeAuto}

// Rule is one line of policy. Principal and Target are a name or "*".
// Service and Operation are patterns (Match); Scope and Resource are path
// patterns (MatchPath). No empty pattern is a wildcard: an empty Service is
// the core group.
type Rule struct {
	ID          string      `json:"id"`
	Effect      Effect      `json:"effect"`
	AccessLevel AccessLevel `json:"accessLevel"`
	Principal   string      `json:"principal"`
	Target      string      `json:"target"`
	Scope       string      `json:"scope"`
	Service     string      `json:"service"`
	Operation   string      `json:"operation"`
	Resource    string      `json:"resource"`
	// Command is a rule one command's answer added, kept in memory for that
	// command alone and never stored.
	Command bool `json:"-"`
}

// Matches is whether r applies to act: an allow of act's level, or a deny or
// ask whose level covers it; its principal and target or "*"; and each
// pattern matching. For a spanning action a deny's or an ask's scope below it
// matches too, and an allow's must reach everything below it.
func (r Rule) Matches(act Action) bool {
	level := r.AccessLevel == act.AccessLevel || r.Effect != Allow && r.AccessLevel.Covers(act.AccessLevel)
	var scope bool
	switch {
	case !act.Spans:
		scope = MatchPath(r.Scope, act.Scope)
	case r.Effect == Allow:
		scope = MatchPathAndBelow(r.Scope, act.Scope)
	default:
		scope = MatchPath(r.Scope, act.Scope) || MatchPathBelow(r.Scope, act.Scope)
	}
	return level &&
		(r.Principal == "*" || r.Principal == string(act.Principal)) &&
		(r.Target == "*" || r.Target == string(act.Target)) &&
		scope &&
		Match(r.Service, act.Service) &&
		Match(r.Operation, act.Operation) &&
		MatchPath(r.Resource, act.Resource)
}

// Same is whether r and o are one rule but for their ids.
func (r Rule) Same(o Rule) bool {
	r.ID, o.ID = "", ""
	return r == o
}

// Account is the account r's first scope segment names, false when that
// segment is a pattern Literal would not have written.
func (r Rule) Account() (string, bool) {
	first, _, _ := strings.Cut(r.Scope, "/")
	return LiteralValue(first)
}

// Limit is the boundary's word on a principal's action at one level, Kubetail's
// and no setting. The limits order Free, Scoped, Once, Never, each more
// restrictive than the one before.
type Limit int

const (
	Free   Limit = iota // the mode and the rules decide
	Scoped              // Free for a Confined action, else Once
	Once                // no rule or mode allows it: it asks each time
	Never               // denied
)

// boundary is each principal's limit at each level. A principal or level with
// no row is Never.
var boundary = map[Principal]map[AccessLevel]Limit{
	Chat:     {Read: Free, DataRead: Free, Write: Free, AccessControl: Scoped, Destructive: Once, Reach: Never},
	Subagent: {Read: Free, DataRead: Free, Write: Free, AccessControl: Scoped, Destructive: Once, Reach: Never},
	Monitor:  {Read: Free, DataRead: Never, Write: Never, AccessControl: Never, Destructive: Never, Reach: Never},
	Console:  {Read: Free, DataRead: Never, Write: Never, AccessControl: Never, Destructive: Never, Reach: Never},
}

// asks is whether a principal has someone to ask. One that does not is denied
// what would ask.
var asks = map[Principal]bool{Chat: true, Subagent: true, Monitor: false, Console: false}

// limitOf is act's limit: its principal's at its level, Scoped resolved by
// Confined, and no less than Once for an action marked Once or with no
// account.
func limitOf(act Action) Limit {
	l, ok := boundary[act.Principal][act.AccessLevel]
	if !ok {
		return Never
	}
	if l == Scoped {
		l = Once
		if act.Confined {
			l = Free
		}
	}
	if (act.Once || act.Scope == "") && l < Once {
		l = Once
	}
	return l
}

// modes is each mode's default effect at each level. A mode or level with no
// row denies.
var modes = map[Mode]map[AccessLevel]Effect{
	ModeReadOnly: {Read: Allow, DataRead: Ask, Write: Deny, AccessControl: Deny, Destructive: Deny, Reach: Deny},
	ModeAsk:      {Read: Allow, DataRead: Ask, Write: Ask, AccessControl: Ask, Destructive: Ask, Reach: Deny},
	ModeAuto:     {Read: Allow, DataRead: Allow, Write: Allow, AccessControl: Ask, Destructive: Ask, Reach: Deny},
}

// Decision is what a proxy does with a verdict.
type Decision string

const (
	Allowed  Decision = "allowed"
	Prompted Decision = "prompted"
	Denied   Decision = "denied"
)

// Duration is how long the user's approval of an action holds: this request,
// the rest of the command that sent it, the chat, or always.
type Duration string

const (
	DurationOnce    Duration = "once"
	DurationCommand Duration = "command"
	DurationChat    Duration = "chat"
	DurationAlways  Duration = "always"
)

// Answer is the user's decision on an action that asked: approved or not, and
// for how long.
type Answer struct {
	Approved bool
	Duration Duration
}

// Verdict is what the policy says of an action before anyone is asked.
type Verdict struct {
	Effect Effect
	// Grantable is an Ask an answer may write a rule for.
	Grantable bool
}

// Outcome is what the proxies do with v: an allow runs, an ask is put to the
// user, and anything else is refused.
func (v Verdict) Outcome() Decision {
	switch v.Effect {
	case Allow:
		return Allowed
	case Ask:
		return Prompted
	}
	return Denied
}

// rank orders verdicts from the most restrictive: deny, an ask no answer may
// grant, a grantable ask, allow.
func (v Verdict) rank() int {
	switch {
	case v.Effect == Allow:
		return 3
	case v.Effect == Ask && v.Grantable:
		return 2
	case v.Effect == Ask:
		return 1
	}
	return 0
}

// Policy is what a session brings to a decision: the account's mode and the
// rules that apply. Their order picks the reason alone, never the verdict.
type Policy struct {
	Mode  Mode
	Rules []Rule
}

// Decide answers what happens to act under p, and why, in the user's words.
func (p Policy) Decide(act Action) (Decision, string) {
	v, why := p.Authorize(act)
	return v.Outcome(), why
}

// Authorize is the policy's verdict on act, and the reason in the user's
// words. A principal with nobody to ask is denied what would ask, and a
// subagent is never wider than its parent: its verdict is the more
// restrictive of its own and the same action's as the chat's.
func (p Policy) Authorize(act Action) (Verdict, string) {
	v, why := p.authorize(act)
	if act.Principal == Subagent {
		parent := act
		parent.Principal = Chat
		if pv, pwhy := p.authorize(parent); pv.rank() < v.rank() {
			return pv, pwhy
		}
	}
	return v, why
}

func (p Policy) authorize(act Action) (Verdict, string) {
	v, why := p.verdict(act)
	if v.Effect == Ask && !asks[act.Principal] {
		return Verdict{Effect: Deny}, "this session never asks: " + why
	}
	return v, why
}

// verdict takes the first step that applies, so which rules match decides it,
// whatever their order.
func (p Policy) verdict(act Action) (Verdict, string) {
	limit := limitOf(act)
	mode, ok := modes[p.Mode][act.AccessLevel]
	if !ok {
		mode = Deny
	}
	if limit == Never {
		return Verdict{Effect: Deny}, "it is never allowed"
	}
	if r, ok := p.first(Deny, act); ok {
		return Verdict{Effect: Deny}, "a rule denies it: " + r.Line(act.AccountName)
	}
	if mode == Deny {
		return Verdict{Effect: Deny}, "this account is read-only"
	}
	if r, ok := p.first(Ask, act); ok {
		return Verdict{Effect: Ask}, "a rule asks for it: " + r.Line(act.AccountName)
	}
	if limit == Once {
		return Verdict{Effect: Ask}, "it always asks"
	}
	if r, ok := p.first(Allow, act); ok {
		return Verdict{Effect: Allow}, "a rule allows it: " + r.Line(act.AccountName)
	}
	if mode == Allow {
		return Verdict{Effect: Allow}, string(p.Mode) + " mode"
	}
	return Verdict{Effect: Ask, Grantable: !act.DryRun && GrantRule(act).Storable()}, string(p.Mode) + " mode"
}

// first is the first rule of effect that matches act.
func (p Policy) first(effect Effect, act Action) (Rule, bool) {
	for _, r := range p.Rules {
		if r.Effect == effect && r.Matches(act) {
			return r, true
		}
	}
	return Rule{}, false
}

// GrantRule is the rule a chat or always answer to act writes: an allow of
// its level on its target, for every principal, since the user approved the
// action whoever sent it, in its scope as a literal, and for a spanning action
// everything below it too. An action whose scope is
// one segment, its account alone, also names its service and resource, since
// a rule over a whole account's objects would be too wide. The writer sets
// the ID.
func GrantRule(act Action) Rule {
	r := Rule{
		Effect: Allow, AccessLevel: act.AccessLevel, Principal: "*", Target: string(act.Target),
		Scope: Literal(act.Scope), Service: "*", Operation: "*", Resource: "*",
	}
	if act.Spans {
		// What it was asked for reaches every scope below its own.
		r.Scope += "/**"
	}
	if !strings.Contains(act.Scope, "/") {
		r.Service, r.Resource = Literal(act.Service), Literal(act.Resource)
	}
	return r
}

// Storable is whether r has every pattern a stored rule needs: a rule naming
// an empty scope, operation or resource is refused on write.
func (r Rule) Storable() bool {
	return r.Scope != "" && r.Operation != "" && r.Resource != ""
}

// CommandRule is the rule a command answer to act adds for the rest of the
// command: GrantRule's, naming the service, operation and resource as well,
// since the user saw one change and allows its repeats.
func CommandRule(act Action) Rule {
	r := GrantRule(act)
	r.Service, r.Operation, r.Resource, r.Command = Literal(act.Service), Literal(act.Operation), Literal(act.Resource), true
	return r
}

// GrantLines are the rules each allow answer to act adds, in words: a command
// answer's, and a chat or always answer's.
func GrantLines(act Action) (command, chat string) {
	return CommandRule(act).Line(act.AccountName), GrantRule(act).Line(act.AccountName)
}

// Refused stands in for rules Kubetail could not read, which may have held a
// deny: every write, everywhere, refused.
var Refused = Rule{ID: "refused", Effect: Deny, AccessLevel: Write, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}

// RefusedData stands beside Refused, since a write rule does not cover a data
// read.
var RefusedData = Rule{ID: "refused-data", Effect: Deny, AccessLevel: DataRead, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}

// Unread is what a read of rules that failed answers: no rule it read, since
// one read before the failure may be an allow a later deny narrowed, and
// Refused and RefusedData.
func Unread() []Rule { return []Rule{Refused, RefusedData} }

var (
	effectWords = map[Effect]string{Allow: "Allow", Deny: "Deny", Ask: "Ask for"}
	levelNouns  = map[AccessLevel]string{
		Read:          "reads",
		DataRead:      "data reads",
		Write:         "writes",
		AccessControl: "access control changes",
		Destructive:   "destructive changes",
		Reach:         "reach",
	}
	// levelWords lead a line that names what a rule covers.
	levelWords = map[AccessLevel]string{DataRead: "data", AccessControl: "access control", Destructive: "destructive"}
)

// Line is the rule in the user's words: <effect> <what> [by <principal>]
// <where>, as "Allow writes in dev-eks / team-a" or "Allow writes in AWS
// 123456789012 / eu-west-1". account is the user's name for the account r's
// first scope segment names, read only when that segment is a literal; ""
// draws the segment itself. Each field is drawn by fieldWords, so " / "
// outside quotes always separates scope segments.
func (r Rule) Line(account string) string {
	line := effectWords[r.Effect] + " " + r.what()
	if r.Principal != "*" {
		line += " by " + fieldWords(r.Principal)
	}
	line += " " + r.where(account)
	if r.Command {
		line += " for this command"
	}
	return line
}

// what is the line's object: the level's noun, or what the rule names of it.
func (r Rule) what() string {
	if r.Service == "*" && r.Operation == "*" && r.Resource == "*" {
		return levelNouns[r.AccessLevel]
	}
	var parts []string
	// An operation names a data read's verb itself: "get of core secrets".
	if w, ok := levelWords[r.AccessLevel]; ok && (r.AccessLevel != DataRead || r.Operation == "*") {
		parts = append(parts, w)
	}
	switch {
	case r.Operation != "*":
		parts = append(parts, fieldWords(r.Operation))
	case r.AccessLevel == Read || r.AccessLevel == DataRead:
		parts = append(parts, "reads")
	default:
		parts = append(parts, "writes")
	}
	parts = append(parts, "of")
	switch {
	case r.Service == "" && (r.Target == string(Kubernetes) || r.Target == "*"):
		parts = append(parts, "core")
	case r.Service != "*":
		parts = append(parts, fieldWords(r.Service))
	}
	switch {
	case r.Resource != "*":
		parts = append(parts, fieldWords(r.Resource))
	case r.Service != "*":
		parts = append(parts, "resources")
	default:
		parts = append(parts, "anything")
	}
	return strings.Join(parts, " ")
}

// where is the line's place: its target, its account and the rest of its
// scope.
func (r Rule) where(account string) string {
	segs := strings.Split(r.Scope, "/")
	below := false
	if last := len(segs) - 1; segs[last] == "**" {
		segs, below = segs[:last], true
	}
	target := Target(r.Target)
	if len(segs) == 0 || len(segs) == 1 && segs[0] == "*" {
		switch {
		case r.Target == "*":
			return "everywhere"
		case target == Kubernetes:
			return "in every cluster"
		}
		return "in every " + targetWords[target] + " account"
	}
	var words []string
	if name, ok := LiteralValue(segs[0]); ok {
		if account != "" {
			name = account
		}
		word := fieldWords(Literal(name))
		if t, ok := targetWords[target]; ok {
			word = t + " " + word
		}
		words = append(words, word)
	} else {
		word := "any account"
		switch {
		case target == Kubernetes:
			word = "any cluster"
		case r.Target != "*":
			word = "any " + targetWords[target] + " account"
		}
		if segs[0] != "*" {
			word += " " + matchingWords(segs[0])
		}
		words = append(words, word)
	}
	for _, s := range segs[1:] {
		words = append(words, fieldWords(s))
	}
	where := strings.Join(words, " / ")
	if below {
		return "anywhere in " + where
	}
	return "in " + where
}

// fieldWords draws one pattern field of a line. Name characters alone are
// drawn bare, a * or ? in them a glob. A literal holding a glob character, a
// space, a " or a \ is drawn unescaped in quotes. Anything else is drawn as
// written, in quotes, after "matching". An empty field is drawn as "".
func fieldWords(v string) string {
	if v == "" {
		return `""`
	}
	if !strings.ContainsAny(v, ` "\`) {
		return v
	}
	if plain, ok := LiteralValue(v); ok {
		return quote(plain)
	}
	return "matching " + quote(v)
}

// matchingWords draws a pattern that is not a literal as what it matches.
func matchingWords(pattern string) string {
	if w := fieldWords(pattern); strings.HasPrefix(w, "matching ") {
		return w
	}
	return "matching " + pattern
}

// quote is s in double quotes, each " and \ inside preceded by \.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

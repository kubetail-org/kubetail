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

package logsinput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/iso8601"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// Input is one call's shared arguments as the model typed them, each checked for
// shape.
type Input struct {
	Description string
	Sources     []Source
	Filters     []tools.LogsViewFilter
	Grep        string
}

// NamesScope is whether the call names any of the scope: sources, filters or
// grep, which a token already holds.
func (in Input) NamesScope() bool {
	return len(in.Sources) > 0 || len(in.Filters) > 0 || in.Grep != ""
}

// CheckMessageBytes refuses a max_message_bytes past what the service keeps: 0
// is the whole message.
func CheckMessageBytes(n int) error {
	if n < 0 || n > logs.MaxMessageBytesCap {
		return BadInput("max_message_bytes", fmt.Sprintf("0 for the whole message, or 1 to %d", logs.MaxMessageBytesCap))
	}
	return nil
}

// Source is one source as typed: its resource read into a kind and a name.
type Source struct {
	Namespace     string
	Kind          tools.LogsSourceKind
	Name          string
	Containers    []string
	AllContainers bool
	Previous      bool
}

// maxSources is the most sources a call names. A source with nothing to read has
// no containers, so the service's cap on containers does not bound it.
const maxSources = 100

// InputError is arguments a tool refuses. Field names the argument when it is one
// the tool knows, "" otherwise; Source is the source it concerns, -1 for none;
// Message, the tool's own words, says what to change.
type InputError struct {
	Field, Message string
	Source         int
}

func (e *InputError) Error() string {
	if e.Field == "" {
		return "logs input is not one object of the tool's own fields"
	}
	return "logs input has a bad " + e.Field
}

// BadInput refuses field, of no one source, saying message.
func BadInput(field, message string) *InputError {
	return &InputError{Field: field, Message: message, Source: -1}
}

// kindOf is each resource as kubectl spells it, plural, singular and short.
var kindOf = map[string]tools.LogsSourceKind{
	"pods": tools.LogsSourcePod, "pod": tools.LogsSourcePod, "po": tools.LogsSourcePod,
	"deployments": tools.LogsSourceDeployment, "deployment": tools.LogsSourceDeployment, "deploy": tools.LogsSourceDeployment,
	"statefulsets": tools.LogsSourceStatefulSet, "statefulset": tools.LogsSourceStatefulSet, "sts": tools.LogsSourceStatefulSet,
	"daemonsets": tools.LogsSourceDaemonSet, "daemonset": tools.LogsSourceDaemonSet, "ds": tools.LogsSourceDaemonSet,
	"jobs": tools.LogsSourceJob, "job": tools.LogsSourceJob,
	"cronjobs": tools.LogsSourceCronJob, "cronjob": tools.LogsSourceCronJob, "cj": tools.LogsSourceCronJob,
	"replicasets": tools.LogsSourceReplicaSet, "replicaset": tools.LogsSourceReplicaSet, "rs": tools.LogsSourceReplicaSet,
}

// filterFields is each filter key and its field, in the order the action lists them.
var filterFields = []struct {
	key   string
	field tools.LogsFilterField
}{
	{"node", tools.LogsFilterNode}, {"region", tools.LogsFilterRegion}, {"zone", tools.LogsFilterZone},
	{"os", tools.LogsFilterOS}, {"arch", tools.LogsFilterArch},
}

// Fields is a tool's own fields beside the shared ones: each key's destination, a
// *string, *bool, *int or *logs.Padding the value is read into, or the InputError
// the key is refused with.
type Fields map[string]any

// Parse reads an object whose keys are the shared fields and own, each spelled
// exactly and at most once, with nothing after it, and checks each value off its
// token, since a typed decode takes null as the zero value. It takes a call with
// no sources: a tool that needs them refuses that itself.
func Parse(raw json.RawMessage, own Fields) (Input, error) {
	in := Input{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	w := walker{dec, raw}
	err := w.object(func(key string) error {
		switch key {
		case "description":
			return w.string(&in.Description, key)
		case "sources":
			return w.array(func(i int) error {
				if i == maxSources {
					return BadInput("sources", fmt.Sprintf("at most %d", maxSources))
				}
				s, err := parseSource(w, i)
				in.Sources = append(in.Sources, s)
				return err
			})
		case "filters":
			return parseFilters(w, &in.Filters)
		case "grep":
			return w.string(&in.Grep, key)
		}
		if dst, ok := own[key]; ok {
			return w.value(dst, key)
		}
		return &InputError{Source: -1}
	})
	if err != nil {
		return Input{}, err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Input{}, &InputError{Source: -1}
	}

	if len(in.Grep) > logline.MaxGrep || strings.ContainsAny(in.Grep, "\r\n") {
		return Input{}, BadInput("grep", "")
	}
	if _, err := regexp.Compile(in.Grep); err != nil {
		return Input{}, BadInput("grep", "does not compile as a regular expression")
	}
	return in, nil
}

// parseSource reads source i: its resource as <kind>/<name>, the namespace and
// each container a DNS name, and all_containers refused beside containers.
func parseSource(w walker, i int) (Source, error) {
	s := Source{}
	var resource string
	err := w.object(func(key string) error {
		switch key {
		case "namespace":
			return w.string(&s.Namespace, key)
		case "resource":
			return w.string(&resource, key)
		case "containers":
			return w.strings(&s.Containers, key)
		case "all_containers":
			return w.bool(&s.AllContainers, key)
		case "previous":
			return w.bool(&s.Previous, key)
		}
		return &InputError{Source: i}
	})
	if err != nil {
		// A document the decoder could not finish names no source: where a
		// syntax error surfaces differs between Go releases.
		if ie, ok := errors.AsType[*InputError](err); ok && json.Valid(w.raw) {
			ie.Source = i
		}
		return Source{}, err
	}
	if !dnsLabel(s.Namespace) {
		return Source{}, &InputError{Field: "namespace", Source: i}
	}
	kind, name, _ := strings.Cut(resource, "/")
	s.Kind, s.Name = kindOf[strings.ToLower(kind)], name
	if s.Kind == "" || !dnsSubdomain(name) {
		return Source{}, &InputError{Field: "resource", Source: i, Message: "<kind>/<name>: pods, deployments, statefulsets, daemonsets, jobs, cronjobs or replicasets"}
	}
	for _, c := range s.Containers {
		if !dnsLabel(c) {
			return Source{}, &InputError{Field: "containers", Source: i}
		}
	}
	if s.AllContainers && len(s.Containers) > 0 {
		return Source{}, &InputError{Field: "all_containers", Source: i, Message: "not with containers"}
	}
	return s, nil
}

// parseFilters reads the filters object into one filter per field set, in the
// action's order, each a non-empty list of non-empty values.
func parseFilters(w walker, out *[]tools.LogsViewFilter) error {
	values := map[string][]string{}
	err := w.object(func(key string) error {
		for _, f := range filterFields {
			if f.key == key {
				var list []string
				if err := w.strings(&list, "filters."+key); err != nil {
					return err
				}
				values[key] = list
				return nil
			}
		}
		return &InputError{Source: -1}
	})
	if err != nil {
		return err
	}
	for _, f := range filterFields {
		list, ok := values[f.key]
		if !ok {
			continue
		}
		if len(list) == 0 {
			return &InputError{Field: "filters." + f.key, Source: -1, Message: "never empty"}
		}
		if slices.Contains(list, "") {
			return &InputError{Field: "filters." + f.key, Source: -1, Message: "never empty"}
		}
		*out = append(*out, tools.LogsViewFilter{Field: f.field, Values: list})
	}
	return nil
}

// walker reads one JSON value, raw, token by token.
type walker struct {
	dec *json.Decoder
	raw []byte
}

// object reads {, each key once and in turn through each, and }.
func (w walker) object(each func(key string) error) error {
	if tok, err := w.dec.Token(); err != nil || tok != json.Delim('{') {
		return &InputError{Source: -1}
	}
	seen := map[string]bool{}
	for w.dec.More() {
		tok, err := w.dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok || seen[key] {
			return &InputError{Source: -1}
		}
		seen[key] = true
		if err := each(key); err != nil {
			return err
		}
	}
	if tok, err := w.dec.Token(); err != nil || tok != json.Delim('}') {
		return &InputError{Source: -1}
	}
	return nil
}

// array reads [, each element in turn through each with its index, and ].
func (w walker) array(each func(i int) error) error {
	if tok, err := w.dec.Token(); err != nil || tok != json.Delim('[') {
		return &InputError{Source: -1}
	}
	for i := 0; w.dec.More(); i++ {
		if err := each(i); err != nil {
			return err
		}
	}
	if tok, err := w.dec.Token(); err != nil || tok != json.Delim(']') {
		return &InputError{Source: -1}
	}
	return nil
}

// value reads the token into dst, one of Fields' destinations.
func (w walker) value(dst any, field string) error {
	switch dst := dst.(type) {
	case *string:
		return w.string(dst, field)
	case *bool:
		return w.bool(dst, field)
	case *int:
		return w.int(dst, field)
	case *logs.Padding:
		return w.padding(dst, field)
	case error:
		return dst
	}
	panic("logsinput: a field reads into a *string, *bool, *int or *logs.Padding")
}

// paddingMessage is the refusal of a padding, naming both forms and their caps.
var paddingMessage = fmt.Sprintf("a number of lines from 0 to %d, or an ISO 8601 duration such as PT5M of at most %s",
	logs.MaxPageSize, iso8601.Format(logs.MaxPaddingSpan))

// padding reads a number as lines, a whole number up to logs.MaxPageSize, or a
// string as a span up to logs.MaxPaddingSpan, refusing anything else.
func (w walker) padding(out *logs.Padding, field string) error {
	refused := &InputError{Field: field, Source: -1, Message: paddingMessage}
	tok, err := w.dec.Token()
	if err != nil {
		return refused
	}
	switch v := tok.(type) {
	case float64:
		if v != math.Trunc(v) || v < 0 || v > logs.MaxPageSize {
			return refused
		}
		*out = logs.Padding{Lines: int(v)}
		return nil
	case string:
		p, err := iso8601.Parse(v)
		if err != nil {
			return refused
		}
		span, err := iso8601.Span(p, logs.MaxPaddingSpan)
		if err != nil {
			return refused
		}
		*out = logs.Padding{Span: span}
		return nil
	}
	return refused
}

// string reads a string into out, refusing any other token as a bad field.
func (w walker) string(out *string, field string) error {
	tok, err := w.dec.Token()
	s, ok := tok.(string)
	if err != nil || !ok {
		return &InputError{Field: field, Source: -1}
	}
	*out = s
	return nil
}

// bool reads a boolean into out, refusing any other token as a bad field.
func (w walker) bool(out *bool, field string) error {
	tok, err := w.dec.Token()
	b, ok := tok.(bool)
	if err != nil || !ok {
		return &InputError{Field: field, Source: -1}
	}
	*out = b
	return nil
}

// int reads a whole number into out, refusing any other token as a bad field.
func (w walker) int(out *int, field string) error {
	tok, err := w.dec.Token()
	f, ok := tok.(float64)
	if err != nil || !ok || f != math.Trunc(f) || math.Abs(f) > 1<<31 {
		return &InputError{Field: field, Source: -1}
	}
	*out = int(f)
	return nil
}

// strings reads an array of strings into out, refusing anything else as a bad field.
func (w walker) strings(out *[]string, field string) error {
	tok, err := w.dec.Token()
	if err != nil || tok != json.Delim('[') {
		return &InputError{Field: field, Source: -1}
	}
	list := []string{}
	for w.dec.More() {
		var s string
		if err := w.string(&s, field); err != nil {
			return err
		}
		list = append(list, s)
	}
	if tok, err := w.dec.Token(); err != nil || tok != json.Delim(']') {
		return &InputError{Field: field, Source: -1}
	}
	*out = list
	return nil
}

// dnsLabel is a name of at most 63 lower-case letters, digits and hyphens, as a
// namespace's and a container's are.
func dnsLabel(s string) bool { return dnsName(s, 63, false) }

// dnsSubdomain is a name of at most 253 lower-case letters, digits, hyphens and
// dots, as an object's is.
func dnsSubdomain(s string) bool { return dnsName(s, 253, true) }

func dnsName(s string, max int, dots bool) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		switch {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', dots && c == '.':
		default:
			return false
		}
	}
	return true
}

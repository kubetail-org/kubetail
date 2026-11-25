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

package githubproxy

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// maxPushOpening is the most of a push's body read before it is asked: the
// pkt-lines naming its refs and options, about 600 refs. The pack after them
// streams to GitHub unread.
const maxPushOpening = 64 << 10

var (
	errPushUnread   = errors.New("the push's opening does not read")
	errPushTooLarge = errors.New("the push's opening is past its bound")
)

// push is what a push's opening names.
type push struct {
	probe   bool // git's probe, one flush packet, which names no ref
	refs    []refUpdate
	options []string
}

// refUpdate is one ref a push moves, each id in full.
type refUpdate struct{ name, old, new string }

// deletes is whether the push deletes a ref.
func (p push) deletes() bool {
	return slices.ContainsFunc(p.refs, func(u refUpdate) bool { return isZero(u.new) })
}

// details are the push's lines on its request: the repository, each ref
// and what happens to it, and each push option.
func (p push) details(repo string) []runproxy.Detail {
	details := []runproxy.Detail{{Label: "Repository", Value: repo}}
	for _, u := range p.refs {
		label := "Updates"
		switch {
		case isZero(u.old):
			label = "Creates"
		case isZero(u.new):
			label = "Deletes"
		}
		details = append(details, runproxy.Detail{Label: label, Value: u.name + " " + u.old + " → " + u.new})
	}
	for _, o := range p.options {
		details = append(details, runproxy.Detail{Label: "Push option", Value: o})
	}
	return details
}

// readPush reads a push's opening from body, at most maxPushOpening: the
// shallow lines, which it skips, the commands, and the options when the
// commands' capabilities name push-options. It answers the bytes it read, so
// the request can be sent whole, or errPushTooLarge past the bound and
// errPushUnread for anything else it cannot read: a push certificate, an id
// that is not one, a ref or option that is not printable UTF-8.
func readPush(body io.Reader) ([]byte, push, error) {
	var opening bytes.Buffer
	pkts := pktReader{io.TeeReader(io.LimitReader(body, maxPushOpening), &opening)}
	p, err := pkts.push(body)
	switch {
	case err == nil:
		return opening.Bytes(), p, nil
	case opening.Len() == maxPushOpening:
		return nil, push{}, errPushTooLarge
	}
	return nil, push{}, errPushUnread
}

// pktReader reads Git's pkt-lines.
type pktReader struct{ r io.Reader }

// push reads a push's opening. body is what pkts reads from, read past the
// bound only to tell git's probe from a flush with more after it.
func (pkts pktReader) push(body io.Reader) (push, error) {
	line, flush, err := pkts.next()
	if err != nil {
		return push{}, err
	}
	if flush {
		if n, _ := io.ReadFull(body, make([]byte, 1)); n != 0 {
			return push{}, errPushUnread
		}
		return push{probe: true}, nil
	}
	for {
		id, ok := strings.CutPrefix(line, "shallow ")
		if !ok {
			break
		}
		if !isID(id) {
			return push{}, errPushUnread
		}
		if line, _, err = pkts.next(); err != nil {
			return push{}, err
		}
	}
	command, caps, _ := strings.Cut(line, "\x00")
	var p push
	for !flush {
		u, ok := parseCommand(command)
		if !ok {
			return push{}, errPushUnread
		}
		p.refs = append(p.refs, u)
		if command, flush, err = pkts.next(); err != nil {
			return push{}, err
		}
	}
	if slices.Contains(strings.Fields(caps), "push-options") {
		for {
			option, flush, err := pkts.next()
			if err != nil {
				return push{}, err
			}
			if flush {
				break
			}
			if !printable(option) {
				return push{}, errPushUnread
			}
			p.options = append(p.options, option)
		}
	}
	return p, nil
}

// next is the next pkt-line's payload, less one trailing newline, or flush
// for a flush packet. A delimiter or any other special packet is an error.
func (pkts pktReader) next() (line string, flush bool, err error) {
	var head [4]byte
	if _, err := io.ReadFull(pkts.r, head[:]); err != nil {
		return "", false, err
	}
	n, err := strconv.ParseUint(string(head[:]), 16, 16)
	switch {
	case err != nil:
		return "", false, errPushUnread
	case n == 0:
		return "", true, nil
	case n < 4:
		return "", false, errPushUnread
	}
	payload := make([]byte, n-4)
	if _, err := io.ReadFull(pkts.r, payload); err != nil {
		return "", false, err
	}
	return strings.TrimSuffix(string(payload), "\n"), false, nil
}

// parseCommand reads "<old> <new> <ref>". A push certificate's opening,
// "push-cert", reads as no command.
func parseCommand(line string) (refUpdate, bool) {
	old, rest, _ := strings.Cut(line, " ")
	next, name, _ := strings.Cut(rest, " ")
	if !isID(old) || len(next) != len(old) || !isID(next) || name == "" || !printable(name) {
		return refUpdate{}, false
	}
	return refUpdate{name: name, old: old, new: next}, true
}

// isID is whether s is a commit id in full: 40 hex digits, or 64 in a
// SHA-256 repository.
func isID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

func isZero(id string) bool { return strings.Trim(id, "0") == "" }

// printable is whether s is UTF-8 with no control character.
func printable(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) })
}

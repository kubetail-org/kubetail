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

package logs

import (
	"bytes"
	"cmp"
	"compress/flate"
	"container/heap"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/amorey/gochan/spsc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// ===== The fetch API and the page engine =====

// A page's limits.
const (
	// MaxPageSize is the most lines one page holds, and DefaultPageSize the lines a
	// page holds when the request names none.
	MaxPageSize     = 500
	DefaultPageSize = 100
	// MaxPageBytesCap is the most a page's messages may sum to.
	MaxPageBytesCap = 1 << 20
	// MaxMessageBytesCap is the longest message a page carries: the file's own cap
	// plus the ellipsis clean writes, so a line the file cut arrives whole at the
	// default cap.
	MaxMessageBytesCap = logline.MaxMessage + len("…")
	// MaxPaddingSpan is the most a span of padding may be.
	MaxPaddingSpan = 7 * 24 * time.Hour
)

// PageFrom is where a window's first page comes from.
type PageFrom string

const (
	PageFromFirst PageFrom = "first" // the window's oldest lines
	PageFromLast  PageFrom = "last"  // the window's newest lines
)

// Padding is how far a window extends past one end of its range, to show the
// lines around it: a number of lines or a span of time. At most one is set; both
// zero means none.
type Padding struct {
	Lines int
	Span  time.Duration
}

// Window is when a read looks: Since and Until, the range's bounds, each a moment
// that includes every line at it, nil for open; and Before and After, its padding.
type Window struct {
	Since, Until  *time.Time
	Before, After Padding
}

// Notice is something this page wants the caller to know: a Code, and a Message
// for people. It is about this page alone; a continuation carries none forward,
// and raises its own.
type Notice struct {
	Code    refusal.Code
	Message string
}

// FetchRequest is one page. A first page sets Scope, what is read, Window, when, and
// PageFrom; with no padding it may set Seen, a line the caller has of each stream,
// so that stream reads from strictly past its line (PageFromFirst) or before it
// (PageFromLast), and every other from the window's edge. A continuation sets
// Cursor, one of a page's Prev, Next or Follow as it came: the cursor holds the
// scope and the window. Beside a Next or Follow it may set Seen, merged into the
// cursor's entries, the newer line standing. PageSize is the
// lines a page holds, 1 to MaxPageSize, 0 for DefaultPageSize. MaxMessageBytes
// cuts each message, 0 for the service's own cap. MaxPageBytes caps the sum of a
// page's messages, 0 for MaxPageBytesCap. The three are read on every page.
// ForModel, read on every page too, passes each message through logline.ForModel
// before the cut, so the cut falls on the redacted text. ServerUID, read on every
// page, is the one server the page may read, "" for whichever the cluster reaches:
// a page resolved against another's cache is cluster.ErrIdentityMismatch.
type FetchRequest struct {
	Scope
	Window
	Seen     []string
	PageFrom PageFrom
	Cursor   *string

	PageSize        int
	MaxMessageBytes int
	MaxPageBytes    int
	ForModel        bool
	ServerUID       string
}

// Page is the page's place in the window, each a page cursor, nil where there is
// none: Prev goes on toward older lines, Next toward newer, and Follow polls a
// live window's end for lines written since.
type Page struct {
	Prev   *string
	Next   *string
	Follow *string
}

// FetchResponse is one page of the window, oldest first. Request is the request as it
// ran: defaults filled, caps applied, and for a continuation the Scope and Window
// the page cursor holds. ResolvedAt is the first page's clock. Notices is never
// nil: empty with nothing to say.
type FetchResponse struct {
	Request    FetchRequest
	ResolvedAt time.Time
	Page       Page
	Records    []logline.Record
	Notices    []Notice
}

// Fetch is one page of q's window on cluster id, answered from the cluster
// cache's log file after filling what the page needs of it from the backends.
// The scope is resolved inside one reading of the cluster's mirror; the reads run
// outside it, over a claim on the server the pods came from.
//
//  1. A bound includes its moment: Since and Until each include every line at
//     that nanosecond. Since after Until is invalid-range.
//  2. Padding is measured from the range's bounds. A span widens a bound by that
//     much time. A count is a distance the page cursor carries: a page moving
//     away from a bound delivers at most Lines less the count past it, one moving
//     toward it delivers the lines between. Only a first page whose own edge is
//     counted reads to find its start; short of Lines, the window starts at the
//     log's edge (padding-short), or where the read was cut, with the edge that
//     way reading the rest.
//  3. Padding past an open bound is measured from the log's edge: none before an
//     open Since (empty-padding); past an open Until, from ResolvedAt, the first
//     page's clock, and a first page reaching the window's end with no line at or
//     past it says empty-padding. Only a first page says either. With no After the
//     window is live while it is polled.
//  4. A record's place in the window is its timestamp against the echoed bounds.
//  5. PageFromFirst reads oldest first from the window's start, PageFromLast newest first
//     from its end.
//  6. A continuation sends its Cursor, and beside a Next or Follow may send Seen,
//     unless the window's After is counted; Prev reads newest first, Next and
//     Follow oldest first, never past the window's edge. Mixed requests are
//     conflicting-parameters; a cursor the service did not write is cursor-invalid,
//     and one whose scope is now refused keeps the scope's code. The scope is
//     resolved again on every page. A workload's pods the mirror no longer holds
//     are read from the file, found by the controller chain filed with their
//     streams.
//  7. Prev is set when a line is known older inside the window, Next when one is
//     known newer. A window is live while its end is open, lies after now, or a
//     counted After is short; its page with nothing newer sets Follow, never
//     beside Next.
//  8. A message over MaxMessageBytes is cut at a character boundary, never inside
//     an SGR sequence, and says the bytes cut in TruncatedBytes. With ForModel
//     it goes through logline.ForModel first, and the cut falls on that text.
//  9. Records are kept from the page's starting edge while their messages fit
//     MaxPageBytes; the first always, with page-byte-limit-exceeded when it alone
//     is over. A dropped record sets the edge it lay past.
//  10. Paging is exact: along one direction no line is repeated or skipped,
//     whatever each continuation's caps, but for a loud second's unread rest,
//     named in stream-skipped.
//  11. Filters apply to every line of the window. Grep applies to the range's
//     lines alone: a padding line passes unfiltered, so a count counts every
//     line and a padding costs its count whatever the grep.
//  12. A late line stamped among lines already delivered is on the next Next or
//     Follow page, since each stream goes on from its own last line.
//  13. Request is q as it ran: defaults filled, caps applied, and on a
//     continuation the scope and window the cursor holds.
func (s *Service) Fetch(ctx context.Context, id cluster.ClusterID, q FetchRequest) (FetchResponse, error) {
	q, err := checkRequest(q)
	if err != nil {
		return FetchResponse{}, err
	}
	c, err := s.cursorOf(q)
	if err != nil {
		return FetchResponse{}, err
	}
	if q.Cursor != nil {
		q.Scope, q.Window = c.Scope, c.Window
	}
	grep, err := logline.CompileGrep(c.Grep)
	if err != nil {
		return FetchResponse{}, err
	}
	first := q.Cursor == nil
	notices := []Notice{}
	if first && c.Since == nil && c.Window.Before != (Padding{}) {
		notices = append(notices, Notice{refusal.CodeEmptyPadding, "there is no padding before an open since"})
	}
	m, stop, found, err := s.read(ctx, id, &c, grep, q)
	if err != nil {
		return FetchResponse{}, err
	}
	notices = append(notices, found...)
	// The trim goes first: a line the caps then drop lies inside the window.
	c.trim(&m)
	notices = append(notices, c.capPage(&m, q)...)
	ahead, behind := c.edges(m)
	page := c.place(m, ahead, behind, first, stop, s.now())
	if first && page.Next == nil && c.afterIsEmpty(m.records) {
		notices = append(notices, Notice{refusal.CodeEmptyPadding, "no line is stamped at or past the first page's clock, where the padding after an open until starts"})
	}
	for _, line := range m.skipped {
		notices = append(notices, Notice{refusal.CodeStreamSkipped, line})
	}
	return FetchResponse{Request: q, ResolvedAt: c.ResolvedAt, Page: page, Records: m.records, Notices: notices}, nil
}

// checkRequest is q with its defaults filled and its caps applied, refusing a
// request whose parts do not go together (rule 6) or whose page is out of bounds.
func checkRequest(q FetchRequest) (FetchRequest, error) {
	maxMessage, err := messageCap(q.MaxMessageBytes)
	switch {
	case (q.PageFrom == "") == (q.Cursor == nil):
		return q, refusal.New(refusal.CodeConflictingParameters, "a request is a first page from first or last, or a page cursor alone")
	case q.PageFrom != "" && q.PageFrom != PageFromFirst && q.PageFrom != PageFromLast:
		return q, refusal.New(refusal.CodeConflictingParameters, fmt.Sprintf("a first page comes from first or last, not %q", q.PageFrom))
	case q.Cursor != nil && (!q.Scope.empty() || q.Window != (Window{})):
		return q, refusal.New(refusal.CodeConflictingParameters, "a page cursor holds its scope and window, so a continuation sends neither")
	case len(q.Seen) > 0 && (q.Before != (Padding{}) || q.After != (Padding{})):
		return q, refusal.New(refusal.CodeConflictingParameters, "seen goes with a first page with no padding")
	case q.PageSize < 0 || q.PageSize > MaxPageSize:
		return q, refusal.New(refusal.CodeBadPage, fmt.Sprintf("a page holds 1 to %d lines", MaxPageSize))
	case err != nil:
		return q, err
	case q.MaxPageBytes < 0:
		return q, errNegativeCap
	}
	if q.PageSize == 0 {
		q.PageSize = DefaultPageSize
	}
	q.MaxMessageBytes = maxMessage
	if q.MaxPageBytes == 0 || q.MaxPageBytes > MaxPageBytesCap {
		q.MaxPageBytes = MaxPageBytesCap
	}
	return q, nil
}

// errNegativeCap refuses a byte cap below 0.
var errNegativeCap = refusal.New(refusal.CodeBadPage, "a byte cap is not negative")

// messageCap is a request's MaxMessageBytes as it runs: 0, and anything over
// MaxMessageBytesCap, is MaxMessageBytesCap; a negative is refused.
func messageCap(n int) (int, error) {
	switch {
	case n < 0:
		return 0, errNegativeCap
	case n == 0, n > MaxMessageBytesCap:
		return MaxMessageBytesCap, nil
	}
	return n, nil
}

// cursorOf is the page cursor q reads from: its Cursor decoded, with q's Seen
// merged into its entries, or a first page's on the service's clock. Seen goes
// only beside a newer cursor, since an older page has nothing to read past, and
// not in a window whose After is counted, since the cursor's count holds the
// pages' padding lines alone and the merged start would deliver them again.
func (s *Service) cursorOf(q FetchRequest) (pageCursor, error) {
	if q.Cursor == nil {
		return firstPage(q, s.now())
	}
	c, err := decodePageCursor(*q.Cursor)
	if err != nil || len(q.Seen) == 0 {
		return c, err
	}
	if !c.newer || c.counted(sideAfter) {
		return pageCursor{}, refusal.New(refusal.CodeConflictingParameters, "seen goes beside a next or follow whose window has no counted after")
	}
	c.seen, err = mergeSeen(c.seen, q.Seen)
	return c, err
}

// read is the page c reads with grep, q.PageSize lines: the range's merge alone,
// or on a first page whose own edge is counted, joined with the padding read run
// beside it in one resolve and claim. What the padding read found goes into c;
// stop is where it was cut short of its count, zero with none.
func (s *Service) read(ctx context.Context, id cluster.ClusterID, c *pageCursor, grep *regexp.Regexp, q FetchRequest) (m merge, stop logline.Cursor, notices []Notice, err error) {
	rng := pageQuery{scope: c.Scope, grep: grep, mode: c.dir(), cur: c, limit: q.PageSize}
	var lookup *pageCursor
	var lines int
	if q.Cursor == nil {
		lookup, lines = c.lookup()
	}
	if lookup == nil {
		ms, err := s.page(ctx, id, q.ServerUID, rng)
		if err != nil {
			return merge{}, logline.Cursor{}, nil, err
		}
		return ms[0], logline.Cursor{}, nil, nil
	}
	ms, err := s.page(ctx, id, q.ServerUID, rng, pageQuery{scope: c.Scope, grep: grep, mode: lookup.dir(), cur: lookup, limit: lines})
	if err != nil {
		return merge{}, logline.Cursor{}, nil, err
	}
	stop, notices = c.found(ms[1], lines)
	return joinReads(ms[1], ms[0], c.newer, q.PageSize), stop, notices, nil
}

// joinReads is a first page's padding read and range read as one page: the
// padding's records beside the range's, the first limit kept in the order the
// page reads, from its starting edge. The cut ahead is the range read's, or the
// last line kept when the keep ended the page. A stream's reach is the range
// read's, or, where the range read read none, the padding read's row nearest the
// bound.
func joinReads(pad, rng merge, newer bool, limit int) merge {
	out := merge{cut: rng.cut, skipped: slices.Clone(rng.skipped), reaches: slices.Clone(rng.reaches)}
	for _, line := range pad.skipped {
		if !slices.Contains(out.skipped, line) {
			out.skipped = append(out.skipped, line)
		}
	}
	for i, r := range pad.reaches {
		if out.reaches[i].last.IsZero() {
			out.reaches[i].last = r.first
		}
	}
	if newer {
		out.records = slices.Concat(pad.records, rng.records)
		if len(out.records) > limit {
			out.records = out.records[:limit]
			out.cut = logline.CursorOf(out.records[limit-1])
		}
		return out
	}
	out.records = slices.Concat(rng.records, pad.records)
	if n := len(out.records); n > limit {
		out.records = out.records[n-limit:]
		out.cut = logline.CursorOf(out.records[0])
	}
	return out
}

// pageQuery is what one direction's page reads: the scope and its compiled grep,
// the direction, the page cursor that says where each stream starts and which
// edge it stops at, nil to read every stream from its start or its end, and the
// limit. grepAll applies the grep to the padding too, as a tail does.
type pageQuery struct {
	scope   Scope
	grep    *regexp.Regexp
	mode    logline.Direction
	cur     *pageCursor
	limit   int
	grepAll bool
}

// need is the lines a stream answers: one past the limit, so the merge can tell a
// page that was cut.
func (q pageQuery) need() int { return q.limit + 1 }

// full is whether a stream holding n lines, past of them past the range's end,
// holds what the page needs: need lines, or on a newer page of a window whose After
// is counted, what is left of the count past the end, since trim drops the rest.
func (q pageQuery) full(n, past int) bool {
	if n >= q.need() {
		return true
	}
	if q.cur == nil || q.mode != logline.Newer || !q.cur.counted(sideAfter) {
		return false
	}
	return past > 0 && past >= q.cur.lines(sideAfter)-q.cur.after
}

// keep is whether the page passes a line stamped ts with message: inside the
// range, one the grep matches; outside it, every line, since the grep searches
// the range alone: the padding, and what a look back past the window's edge
// meets.
func (q pageQuery) keep(ts int64, message string) bool {
	if q.cur != nil && !q.grepAll && !q.cur.inRange(ts) {
		return true
	}
	return logline.Matches(q.grep, message)
}

// kept is keep over a record a backend read.
func (q pageQuery) kept(rec logline.Record) bool {
	return q.keep(rec.Timestamp.UnixNano(), rec.Message)
}

// pastEnd is whether p lies past the range's end, which only a counted After's
// lines do on a page full counts them for.
func (q pageQuery) pastEnd(p logstore.Pos) bool {
	if q.cur == nil {
		return false
	}
	end := q.cur.rangeEnd()
	return !end.At.IsZero() && p.TS > end.At.Timestamp.UnixNano()
}

// key is where the page starts in ps.
func (q pageQuery) key(ps pageStream) logstore.Pos {
	if q.cur == nil {
		return logline.StreamPos(logline.Bound{}, ps.run(), q.mode)
	}
	return q.cur.keyFor(ps)
}

// far is where the page stops in ps, the first place past its range there: the
// window's edge ahead read as the bound of a page the other way. Nil with no edge.
func (q pageQuery) far(ps pageStream) *logstore.Pos {
	if q.cur == nil || q.cur.farEdge().At.IsZero() {
		return nil
	}
	far := logline.StreamPos(q.cur.farEdge(), ps.run(), q.mode.Back())
	return &far
}

// page is a page of a scope in progress, read one or more ways: one read, or a
// first page's range read and padding read. It files the resolve's streams and the
// file's, fills the gaps each resolve stream needs from the backends for every
// direction, writing each window of a read once it is whole, then answers each
// direction with one walk of the file per stream, merged.
type page struct {
	s       *Service
	id      cluster.ClusterID
	t       target
	file    *logstore.Store
	streams []pageStream
	// backends is the cluster's backends by name, in the order a gap is offered to
	// them, and walkShare each stream's share of the walks' budget.
	backends  []string
	walkShare int64
	// serverUID is the one server the page may read, "" for any.
	serverUID string
}

// page is one merge per query of a scope on cluster id, every query over one resolve
// of it and one claim. The queries share their scope. A page pinned to serverUID
// ("" for any) resolves against no other server's cache.
func (s *Service) page(ctx context.Context, id cluster.ClusterID, serverUID string, qs ...pageQuery) ([]merge, error) {
	t, err := resolve(ctx, s.clusters, id, serverUID, qs[0].scope, false)
	if err != nil {
		return nil, err
	}
	file, err := s.files.OpenOrCreate(int64(t.cacheID))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	streams, release, err := s.pageStreams(ctx, file, t, qs)
	defer release()
	if err != nil {
		return nil, err
	}
	return s.pageOver(ctx, id, t, file, streams, serverUID, qs...)
}

// reachedAnotherServer is whether a fill's connection reached another server
// than its claim's.
func reachedAnotherServer(fills []streamFill) bool {
	return slices.ContainsFunc(fills, func(f streamFill) bool { return errors.Is(f.err, cluster.ErrIdentityMismatch) })
}

// pageOver is one merge per query over streams, which t resolved or file alone
// holds: their gaps filled from the backends, then each query walked and merged.
// A page for one server (serverUID, "" for any) is refused when a stream's
// connection reaches another, where a page for any skips that stream.
func (s *Service) pageOver(ctx context.Context, id cluster.ClusterID, t target, file *logstore.Store, streams []pageStream, serverUID string, qs ...pageQuery) ([]merge, error) {
	for i := range streams {
		streams[i].tail, streams[i].committed = s.tailOf(t.cacheID, streams[i].row.ID)
	}
	out := make([]merge, len(qs))
	if len(streams) == 0 {
		if t.noNodes != nil {
			return nil, t.noNodes
		}
		for i := range out {
			out[i] = merge{records: []logline.Record{}, skipped: []string{}}
		}
		return out, nil
	}
	p := &page{s: s, id: id, t: t, file: file, streams: streams, walkShare: s.walkBudget / int64(len(streams)), serverUID: serverUID}
	for _, b := range s.backends {
		p.backends = append(p.backends, b.name)
	}
	fills, err := p.fill(ctx, qs)
	if err != nil {
		return nil, err
	}
	wrote := false
	for i := range streams {
		restarted := false
		for _, byStream := range fills {
			if byStream[i].gone {
				streams[i].st = nil
			}
			wrote = wrote || byStream[i].wrote
			restarted = restarted || errors.Is(byStream[i].err, errRestarted)
		}
		// A page answers once for each stream, so a restart one direction found skips
		// it in every direction.
		if restarted {
			for _, byStream := range fills {
				byStream[i] = streamFill{err: errRestarted}
			}
		}
	}
	if wrote && s.afterFill != nil {
		s.afterFill()
	}
	for i, q := range qs {
		if out[i], err = p.answer(ctx, q, fills[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// backendsOf is the backends that serve ps, by name: none for a stream the file
// alone holds.
func (p *page) backendsOf(ps pageStream) []string {
	if ps.exhausted() {
		return nil
	}
	return p.backends
}

// fill fills each stream the resolve handed out for each query, over one claim on
// the server the pods came from; with none to fill, it takes no claim. A newer
// query fills first: an older one from the same bound, a first page's padding read
// before Since or its range read before Until, links each stream's read back from
// the bound up to the newer fill's first line, so the two chunks join there.
func (p *page) fill(ctx context.Context, qs []pageQuery) ([][]streamFill, error) {
	fills := make([][]streamFill, len(qs))
	if !slices.ContainsFunc(p.streams, func(ps pageStream) bool { return !ps.exhausted() }) {
		for i := range fills {
			fills[i] = make([]streamFill, len(p.streams))
		}
		return fills, nil
	}
	c, err := kubelet.Acquire(ctx, p.s.clusters, p.id, p.t.serverUID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	backends := make([]kubelet.Backend, len(p.s.backends))
	for i, b := range p.s.backends {
		backends[i] = b.build(c.Get)
	}
	var seams []*logstore.Pos
	for _, mode := range []logline.Direction{logline.Newer, logline.Older} {
		for i, q := range qs {
			if q.mode != mode {
				continue
			}
			if fills[i], err = p.fillQuery(ctx, q, backends, seams); err != nil {
				return nil, err
			}
			if mode == logline.Newer {
				seams = p.seams(fills[i])
			}
		}
	}
	return fills, nil
}

// fillQuery fills each stream for q, all at once, each under the query's limits: an
// allowance of its own, and the pool they share. seams is each stream's line just past q's bound, nil for none. The
// first fill to fail ends the others, since its error refuses the page; for a
// pinned page, a stream that reached another server refuses it ahead of any cancel.
func (p *page) fillQuery(ctx context.Context, q pageQuery, backends []kubelet.Backend, seams []*logstore.Pos) ([]streamFill, error) {
	fills := make([]streamFill, len(p.streams))
	var filled []int
	for i, ps := range p.streams {
		if !ps.exhausted() {
			filled = append(filled, i)
		}
	}
	limits := kubelet.QueryLimits(p.s.caps, max(len(filled), 1))
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	for n, i := range filled {
		ps := p.streams[i]
		f := &filler{
			file: p.file, clusters: p.s.clusters, id: p.id, serverUID: p.serverUID, save: p.save, ret: p.s.files.Retention(), ps: ps, backends: backends, names: p.backends,
			q: q, limits: limits[n], walk: newBudget(p.walkShare),
		}
		if seams != nil {
			f.seam = seams[i]
		}
		wg.Go(func() {
			var err error
			if fills[i], err = f.run(ctx, q.key(ps)); err != nil {
				cancel(err)
			}
			fills[i].downloaded = f.limits.Spent
			// However early the fill ended, what it holds goes back for the streams
			// still reading.
			f.limits.Ended()
		})
	}
	wg.Wait()
	if err := fillsErr(p.serverUID, fills, context.Cause(ctx)); err != nil {
		return nil, err
	}
	return fills, nil
}

// fillsErr is what refuses a query's page, given its fills and cause, whatever
// cancelled them: for a page pinned to serverUID, a stream that reached another
// server, ahead of cause, which a sibling's failure or the page's deadline may be.
func fillsErr(serverUID string, fills []streamFill, cause error) error {
	if serverUID != "" && reachedAnotherServer(fills) {
		return cluster.ErrIdentityMismatch
	}
	return cause
}

// save writes one batch's fills, stamped: a sweep between the write and the page's
// walk would otherwise take what it fetched, and the stamp keeps the read's lines
// past the page for the next scroll.
func (p *page) save(ctx context.Context, fills []logstore.Fill) (logstore.Written, error) {
	return p.file.Write(ctx, fills, true)
}

// seams is each stream's line a newer query's fill proved nothing lies before past
// its bound, nil where it proved none: an older read back from the bound links up to it.
func (p *page) seams(fills []streamFill) []*logstore.Pos {
	out := make([]*logstore.Pos, len(fills))
	for i, f := range fills {
		out[i] = f.up
	}
	return out
}

// answer walks each stream of the file from q's key in its direction, in one read
// transaction, and merges the walks into the page.
func (p *page) answer(ctx context.Context, q pageQuery, fills []streamFill) (merge, error) {
	readings := make([]*kubelet.Read, len(p.streams))
	reaches := make([]streamReach, len(p.streams))
	var read []logstore.Chunk
	left := false
	err := p.file.Read(ctx, func(r logstore.Reader) error {
		for i, ps := range p.streams {
			w := walker{
				r: r, ps: ps, names: p.backendsOf(ps), q: q, budget: newBudget(p.walkShare + fills[i].downloaded), fill: fills[i],
				look: p.s.files.Retention().LookBytes,
			}
			walked, err := w.walk(q.key(ps))
			if err != nil {
				return err
			}
			readings[i], reaches[i] = walked.reading, walked.reach
			read = append(read, walked.read...)
			left = left || walked.left
		}
		return nil
	})
	if err != nil {
		return merge{}, err
	}
	// A stamp is what keeps the chunks a viewer reads out of eviction; one that fails
	// costs a refill later, never this page.
	if err := p.file.Stamp(ctx, read); err != nil {
		slog.Warn("logs: a page's chunks could not be stamped", "err", err)
	}
	m, err := mergePage(ctx, readings, q.mode, q.limit)
	if err != nil {
		return merge{}, err
	}
	m.skipped, m.reaches = []string{}, reaches
	// failed is the streams skipped for a failure: one stopped at a loud second cut
	// the page, and does not count toward refusing it.
	failed := 0
	for i, f := range fills {
		if f.err != nil {
			m.skipped = append(m.skipped, skippedLine(p.streams[i].key, f.err))
			if kubelet.FailureKind(f.err) != kubelet.FailLimit {
				failed++
			}
		}
	}
	if failed == len(p.t.streams) && len(m.records) == 0 {
		if err := everyStreamFailed(m.skipped); err != nil {
			return merge{}, err
		}
	}
	// A skipped stream does not cut the page, but the page reads to the end of its
	// direction only once every stream has nothing left that way: the next page goes
	// on from the farthest line this one answered. With none, the next page would be
	// this one, so the page is refused instead.
	if m.cut.IsZero() && left {
		if len(m.records) == 0 {
			return merge{}, refusal.New(refusal.CodeReadFailed, "Kubetail could not read "+string(q.mode)+" lines of this view: "+strings.Join(m.skipped, "; "))
		}
		m.cut = logline.CursorOf(m.records[len(m.records)-1])
		if q.mode == logline.Older {
			m.cut = logline.CursorOf(m.records[0])
		}
	}
	return m, nil
}

// ===== The page cursor =====

// pageCursor is which way the pages go on, and where from. The embedded Scope
// and resolvedWindow are the first page's, echoed on every continuation; the
// scope is resolved again on every read. newer is the way this cursor reads.
// seen is where each stream goes on from, exclusive: the last line of it the
// pages passed that way, one entry per stream, the stream read off the entry;
// the same list a caller sends as a request's Seen. A stream with no entry goes
// on from at, exclusive, a line or a moment; with at zero, from the window's
// Start, since nothing of that stream has been shown. A newer cursor carries
// entries, and so does an older one from a first page that sent Seen. Only a
// newer one going on from a page read the other way carries both entries and at.
// before and after are distances, for a counted Padding: the padding lines that
// lie between Since and this cursor's position, and between it and Until; 0 for a
// position inside the range. A side whose edge is not yet a position is paged by
// its count alone.
type pageCursor struct {
	Scope
	resolvedWindow

	newer         bool
	seen          map[logline.ContainerName]logline.RunPlace
	at            logline.Cursor
	before, after int
}

// firstPage is the page cursor a first page reads from: the window's edge its
// PageFrom names, on the clock now, its entries the request's Seen. A Since that
// is a time, or that a span widens, pins the window's start; a counted one waits
// for the read that finds it. now is kept in UTC with no monotonic reading, as a
// continuation decodes it, so every page reports the same ResolvedAt.
func firstPage(q FetchRequest, now time.Time) (pageCursor, error) {
	if err := checkWindow(q.Window); err != nil {
		return pageCursor{}, err
	}
	newer := q.PageFrom == PageFromFirst
	seen, err := seenOf(q.Seen, newer)
	if err != nil {
		return pageCursor{}, err
	}
	c := pageCursor{
		Scope: q.Scope, resolvedWindow: resolvedWindow{Window: q.Window, ResolvedAt: now.UTC().Round(0)},
		newer: newer, seen: seen,
	}
	if q.Since != nil && q.Before.Lines == 0 {
		c.Start = logline.MomentAt(q.Since.Add(-q.Before.Span))
	}
	return c, nil
}

// errBadSeen refuses a Seen entry that does not name a line.
var errBadSeen = refusal.New(refusal.CodeCursorInvalid, "a seen entry is a line cursor a record carried")

// seenOf is a request's Seen as a cursor's entries: each the line the caller has
// of its stream nearest the read, whatever their order. Of two for one stream the
// newer stands on a newer read and the older on an older one, so no stream reads
// a line the caller holds again.
func seenOf(seen []string, newer bool) (map[logline.ContainerName]logline.RunPlace, error) {
	if len(seen) == 0 {
		return nil, nil
	}
	out := make(map[logline.ContainerName]logline.RunPlace, len(seen))
	for _, s := range seen {
		c, err := logline.Decode(s)
		if err != nil {
			return nil, err
		}
		if c.IsMoment() {
			return nil, errBadSeen
		}
		keepFarther(out, c, newer)
	}
	return out, nil
}

// keepFarther sets c's stream's entry in entries to c, unless the entry lies
// farther the way the read goes: newer on a newer read, older on an older one.
func keepFarther(entries map[logline.ContainerName]logline.RunPlace, c logline.Cursor, newer bool) {
	e, ok := entries[c.ContainerName]
	if ok {
		order := logline.CompareCursors(c, logline.Cursor{ContainerName: c.ContainerName, RunPlace: e})
		if newer && order < 0 || !newer && order > 0 {
			return
		}
	}
	entries[c.ContainerName] = c.RunPlace
}

// mergeSeen is entries, which the caller owns, with a request's Seen merged in,
// the newer line of a stream standing: Seen goes only beside a newer cursor.
func mergeSeen(entries map[logline.ContainerName]logline.RunPlace, seen []string) (map[logline.ContainerName]logline.RunPlace, error) {
	more, err := seenOf(seen, true)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return more, nil
	}
	for name, place := range more {
		keepFarther(entries, logline.Cursor{ContainerName: name, RunPlace: place}, true)
	}
	return entries, nil
}

// lookup is the padding read a first page runs beside its range read when its
// own edge is counted, and the lines it reads: back from Since for PageFromFirst, on
// from the range's end for PageFromLast, each stream from the bound. Nil for an edge
// that is a time or a span.
func (c pageCursor) lookup() (*pageCursor, int) {
	var l pageCursor
	switch {
	case c.newer && c.counted(sideBefore):
		l = c.turned(logline.MomentAt(*c.Since))
	case !c.newer && c.counted(sideAfter):
		l = c.turned(c.rangeEnd().At)
	default:
		return nil, 0
	}
	return &l, c.lines(l.away())
}

// dir is the way c reads.
func (c pageCursor) dir() logline.Direction {
	if c.newer {
		return logline.Newer
	}
	return logline.Older
}

// turned is c reading the other way from at, with no entry.
func (c pageCursor) turned(at logline.Cursor) pageCursor {
	c.newer, c.seen, c.at = !c.newer, nil, at
	return c
}

// away is the side c moves away from as it reads.
func (c pageCursor) away() side {
	if c.newer {
		return sideAfter
	}
	return sideBefore
}

// count is the padding lines c has passed on side s.
func (c pageCursor) count(s side) int {
	if s == sideBefore {
		return c.before
	}
	return c.after
}

// nearEdge is the window's known edge c reads away from, zero while it is open or
// a count's edge is still to be found.
func (c pageCursor) nearEdge() logline.Bound {
	if c.newer {
		return c.start()
	}
	return c.end()
}

// farEdge is the window's known edge c reads toward, zero while it is open or a
// count's edge is still to be found.
func (c pageCursor) farEdge() logline.Bound {
	if c.newer {
		return c.end()
	}
	return c.start()
}

// keyFor is where ps's read starts: past its entry, else past at, else from the
// window's edge that way, a counted edge still to be found read as its range
// bound. A key short of the window's known near edge is clipped to it, so a page
// never crosses the window's edge. While that edge is unknown, a newer cursor's
// entry short of at is passed over for at.
func (c pageCursor) keyFor(ps pageStream) logstore.Pos {
	run := ps.run()
	b := c.nearEdge()
	if b.At.IsZero() {
		b = c.rangeStart()
		if !c.newer {
			b = c.rangeEnd()
		}
	}
	if !c.at.IsZero() {
		b = logline.Bound{At: c.at, Inclusive: false}
	}
	// While a newer cursor's near edge is unknown, at is its floor: the lines before
	// it are the older pages'.
	floor := c.newer && c.nearEdge().At.IsZero() && !c.at.IsZero()
	if e, ok := c.seen[run.ContainerName]; ok {
		entry := logline.Cursor{ContainerName: run.ContainerName, RunPlace: e}
		if !floor || logline.CompareCursors(entry, c.at) >= 0 {
			b = logline.Bound{At: entry, Inclusive: false}
		}
	}
	key := logline.StreamPos(b, run, c.dir())
	edge := c.nearEdge()
	switch {
	case edge.At.IsZero():
		return key
	case c.newer:
		return logstore.MaxPos(key, logline.StreamPos(edge, run, c.dir()))
	}
	return logstore.MinPos(key, logline.StreamPos(edge, run, c.dir()))
}

// found is what a first page's padding read m, of lines asked for, tells c: the
// side's count is the lines found. Short of lines, the read was cut, at a moment
// or at a stream skipped on a transient failure, which is answered as stop and,
// for a counted Before, where a stream with no entry goes on from; or it reached
// the log's edge. Otherwise a counted Before's start is its farthest line found,
// or Since with none.
func (c *pageCursor) found(m merge, lines int) (stop logline.Cursor, notices []Notice) {
	k := len(m.records)
	if k < lines {
		stop = m.cut
	}
	if !c.newer {
		c.after = k
		return stop, nil
	}
	c.before = k
	if !stop.IsZero() {
		c.at = stop
		return stop, nil
	}
	c.Start = logline.MomentAt(*c.Since)
	if k > 0 {
		c.Start = logline.CursorOf(m.records[0])
	}
	if k < lines {
		notices = append(notices, Notice{refusal.CodePaddingShort, fmt.Sprintf("the padding before since holds %d of the %d lines asked for", k, lines)})
	}
	return stop, notices
}

// trim keeps at most what remains of a counted side c moves away from while its
// edge is a count, Lines less the count, and drops the rest of m's records: they
// lie past the window, so m is no longer cut. That side's lines sort past every
// other line the page reads, so they lie at the page's far end.
func (c pageCursor) trim(m *merge) {
	s := c.away()
	if !c.counted(s) || s == sideBefore && !c.Start.IsZero() {
		return
	}
	past := 0
	for _, rec := range m.records {
		if c.side(rec) == s {
			past++
		}
	}
	drop := past - (c.lines(s) - c.count(s))
	if drop <= 0 {
		return
	}
	if c.newer {
		m.records = m.records[:len(m.records)-drop]
	} else {
		m.records = m.records[drop:]
	}
	m.cut = logline.Cursor{}
}

// closed is whether c sits at the window's edge on the side it moves away from:
// every line of a counted side lies between it and the bound.
func (c pageCursor) closed() bool {
	s := c.away()
	return c.counted(s) && c.count(s) == c.lines(s)
}

// live is whether more lines may come past c's window's end: its end is open or
// lies after now, or a counted After is short of its Lines (rule 7).
func (c pageCursor) live(now time.Time) bool {
	if c.Window.After.Lines > 0 {
		return c.after < c.Window.After.Lines
	}
	end := c.end()
	return end.At.IsZero() || end.At.Timestamp.After(now)
}

// edges is where the pages go on from after c read m: ahead, the way c reads, and
// behind, the other way, nil when the page delivered no line. m is the page as
// delivered: its records after every trim and cap, and its cut, zero when nothing
// was cut ahead. Each edge's counts are the padding lines between its position
// and each bound.
//
// Going newer, each stream goes on from its reach, the last line it passed, so a
// stream whose clock runs behind loses no line; at goes on as it came. Going
// older, history does not move, so one position is exact: the cut, else the
// farthest line passed. Entries move to each stream's reach either way, so a
// stream whose clock runs behind never reads back from that position. Behind a
// newer page, the page's oldest line; behind an older one, each stream's newest
// line on the page, and the farthest line passed for a stream with none, so
// nothing the page showed is repeated.
func (c pageCursor) edges(m merge) (ahead pageCursor, behind *pageCursor) {
	ahead = c
	ahead.before, ahead.after = c.moved(m.records)
	ahead.seen = c.reached(m)
	var far logline.Cursor
	if !c.newer {
		far = farthest(logline.Older, m)
		ahead.at = cmp.Or(m.cut, far, c.at)
	}
	if len(m.records) == 0 {
		return ahead, nil
	}
	if c.newer {
		back := c.turned(logline.CursorOf(m.records[0]))
		return ahead, &back
	}
	back := c.turned(far)
	back.seen = newestOnPage(m.records)
	return ahead, &back
}

// moved is c's counts once the page delivered recs: plus the padding lines it
// delivered moving away from a bound, minus those moving toward one, clamped to
// 0 through each side's Lines, the backstop for a late line's one-line slip.
func (c pageCursor) moved(recs []logline.Record) (before, after int) {
	before, after = c.before, c.after
	for _, rec := range recs {
		s := c.side(rec)
		if s == sideRange {
			continue
		}
		n := -1
		if s == c.away() {
			n = 1
		}
		if s == sideBefore {
			before += n
		} else {
			after += n
		}
	}
	return clamp(before, c.Window.Before.Lines), clamp(after, c.Window.After.Lines)
}

// clamp is n held to 0 through lines.
func clamp(n, lines int) int { return max(0, min(n, lines)) }

// reached is c's entries once m passed: each stream the page resolved at its
// reach the way c reads, the farther of two runs of one name standing, and each
// other stream at its incoming entry. An entry for a stream that left the scope
// is dropped. An older cursor has entries only from a first page's Seen.
func (c pageCursor) reached(m merge) map[logline.ContainerName]logline.RunPlace {
	if !c.newer && len(c.seen) == 0 {
		return nil
	}
	out := map[logline.ContainerName]logline.RunPlace{}
	passed := map[logline.ContainerName]logline.Cursor{}
	dir := c.dir()
	for _, r := range m.reaches {
		name := r.run.ContainerName
		if e, ok := c.seen[name]; ok {
			out[name] = e
		}
		if p := r.passed(dir, m); !p.IsZero() && (passed[name].IsZero() || beyond(dir, p, passed[name])) {
			passed[name] = p
		}
	}
	for name, p := range passed {
		out[name] = p.RunPlace
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// passed is the farthest line of r's stream the page m passed read dir: the first
// of its stop, its last row, and its last record on the page that does not lie
// past m's cut; zero with none. A row past the cut was not passed, since another
// stream cut the page short of it.
func (r streamReach) passed(dir logline.Direction, m merge) logline.Cursor {
	short := func(p logline.Cursor) bool { return !p.IsZero() && (m.cut.IsZero() || !beyond(dir, p, m.cut)) }
	switch {
	case short(r.stop):
		return r.stop
	case short(r.last):
		return r.last
	}
	if p := r.lastOnPage(dir, m.records); short(p) {
		return p
	}
	return logline.Cursor{}
}

// lastOnPage is the farthest line of r's run among recs read dir, zero with none.
func (r streamReach) lastOnPage(dir logline.Direction, recs []logline.Record) logline.Cursor {
	var out logline.Cursor
	for _, rec := range recs {
		c := logline.CursorOf(rec)
		if c.ContainerName == r.run.ContainerName && c.ContainerID == r.run.ContainerID && (out.IsZero() || beyond(dir, c, out)) {
			out = c
		}
	}
	return out
}

// farthest is the farthest line any stream of m passed read dir, zero with none.
func farthest(dir logline.Direction, m merge) logline.Cursor {
	var out logline.Cursor
	for _, r := range m.reaches {
		if p := r.passed(dir, m); !p.IsZero() && (out.IsZero() || beyond(dir, p, out)) {
			out = p
		}
	}
	return out
}

// newestOnPage is each stream's newest line among recs, the later of two runs of
// one name standing.
func newestOnPage(recs []logline.Record) map[logline.ContainerName]logline.RunPlace {
	newest := map[logline.ContainerName]logline.Cursor{}
	for _, rec := range recs {
		c := logline.CursorOf(rec)
		if cur, ok := newest[c.ContainerName]; !ok || beyond(logline.Newer, c, cur) {
			newest[c.ContainerName] = c
		}
	}
	out := make(map[logline.ContainerName]logline.RunPlace, len(newest))
	for name, c := range newest {
		out[name] = c.RunPlace
	}
	return out
}

// place is the page's place in the window once c read m, from its edges. Ahead,
// the way c reads, is a cursor when a line lies past the page that way inside
// the window; a newer page in a live window with none sets Follow instead.
// Behind is a cursor when the page was a continuation, or on a first page whose
// padding read stopped short at stop, which the edge that way reads on from; an
// older first page in a live window sets Follow from it instead, the poll.
func (c pageCursor) place(m merge, ahead pageCursor, behind *pageCursor, first bool, stop logline.Cursor, now time.Time) Page {
	var p Page
	onward, backward := &p.Next, &p.Prev
	if !c.newer {
		onward, backward = &p.Prev, &p.Next
	}
	switch {
	case ahead.closed():
	case !m.cut.IsZero():
		*onward = encoded(ahead)
	case c.newer && ahead.live(now):
		p.Follow = encoded(ahead)
	}
	switch {
	case first && !stop.IsZero():
		*backward = encoded(c.turned(stop))
	case !first && behind != nil:
		*backward = encoded(*behind)
	case first && !c.newer:
		poll := c.turned(logline.Cursor{})
		if behind != nil {
			poll = *behind
		}
		if poll.live(now) {
			p.Follow = encoded(poll)
		}
	}
	return p
}

// encoded is c as a page cursor.
func encoded(c pageCursor) *string {
	s := encodePageCursor(c)
	return &s
}

// pageCursorVersion is the version a page cursor's JSON carries first.
const pageCursorVersion = 1

// maxPageCursorInflate bounds a page cursor's decompression, far above any cursor
// the service writes.
const maxPageCursorInflate = 16 << 20

// pageCursorJSON is a page cursor as JSON, its keys in the order written. Every
// position is a line cursor's own JSON object.
type pageCursorJSON struct {
	V      int                  `json:"v"`
	Scope  Scope                `json:"s"`
	Window windowJSON           `json:"w"`
	Newer  bool                 `json:"n"`
	Seen   []logline.CursorJSON `json:"seen,omitempty"`
	At     *logline.CursorJSON  `json:"at,omitempty"`
	Before int                  `json:"b"`
	After  int                  `json:"a"`
}

// windowJSON is a resolvedWindow as JSON.
type windowJSON struct {
	Since      *time.Time          `json:"since,omitempty"`
	Until      *time.Time          `json:"until,omitempty"`
	Before     paddingJSON         `json:"before"`
	After      paddingJSON         `json:"after"`
	ResolvedAt time.Time           `json:"resolvedAt"`
	Start      *logline.CursorJSON `json:"start,omitempty"`
}

// paddingJSON is a Padding as JSON, its span in nanoseconds.
type paddingJSON struct {
	Lines int           `json:"lines,omitempty"`
	Span  time.Duration `json:"span,omitempty"`
}

// errBadPageCursor refuses a page cursor this service did not write.
var errBadPageCursor = refusal.New(refusal.CodeCursorInvalid, "this is not a page cursor a page handed back")

// deflaters reuses flate writers: each holds several hundred KB of state, and a
// page encodes up to three cursors.
var deflaters = sync.Pool{New: func() any {
	w, _ := flate.NewWriter(nil, flate.BestCompression) // a valid level never fails
	return w
}}

// encodePageCursor is c as a page cursor: its JSON, deflated, base64url with no
// padding. The entries are written in name order and the bounds in UTC, so one
// page has one string.
func encodePageCursor(c pageCursor) string {
	j := pageCursorJSON{
		V: pageCursorVersion, Scope: c.Scope, Newer: c.newer, Before: c.before, After: c.after,
		Window: windowJSON{
			Since: inUTC(c.Since), Until: inUTC(c.Until), ResolvedAt: c.ResolvedAt.UTC(),
			Before: paddingJSON(c.Window.Before), After: paddingJSON(c.Window.After),
			Start: optionalJSON(c.Start),
		},
		At: optionalJSON(c.at),
	}
	for name, place := range c.seen {
		j.Seen = append(j.Seen, logline.Cursor{ContainerName: name, RunPlace: place}.ToJSON())
	}
	slices.SortFunc(j.Seen, func(a, b logline.CursorJSON) int {
		return logline.CompareNames(logline.ContainerName{Namespace: a.Namespace, Pod: a.Pod, Container: a.Container}, logline.ContainerName{Namespace: b.Namespace, Pod: b.Pod, Container: b.Container})
	})
	raw, err := json.Marshal(j)
	if err != nil { // coverage:ignore plain values always marshal
		panic(err)
	}
	var b bytes.Buffer
	w := deflaters.Get().(*flate.Writer)
	defer deflaters.Put(w)
	w.Reset(&b)
	_, _ = w.Write(raw) // a bytes.Buffer never fails
	_ = w.Close()
	return base64.RawURLEncoding.EncodeToString(b.Bytes())
}

// inUTC is t in UTC, nil for nil.
func inUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// optionalJSON is c's JSON, nil when c is zero.
func optionalJSON(c logline.Cursor) *logline.CursorJSON {
	if c.IsZero() {
		return nil
	}
	j := c.ToJSON()
	return &j
}

// decodePageCursor is the page cursor s spells, checked as a parsed request is.
// Its scope is not checked here: the read resolves it, and keeps that refusal's
// code.
func decodePageCursor(s string) (pageCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return pageCursor{}, errBadPageCursor
	}
	raw, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(b)), maxPageCursorInflate+1))
	if err != nil || len(raw) > maxPageCursorInflate {
		return pageCursor{}, errBadPageCursor
	}
	var j pageCursorJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return pageCursor{}, errBadPageCursor
	}
	if j.V != pageCursorVersion {
		return pageCursor{}, logline.ErrCursorVersion
	}
	c := pageCursor{
		Scope: j.Scope, newer: j.Newer, before: j.Before, after: j.After,
		resolvedWindow: resolvedWindow{
			Window: Window{
				Since: inUTC(j.Window.Since), Until: inUTC(j.Window.Until),
				Before: Padding(j.Window.Before), After: Padding(j.Window.After),
			},
			ResolvedAt: j.Window.ResolvedAt.UTC(),
		},
	}
	if c.Start, err = fromOptionalJSON(j.Window.Start); err != nil {
		return pageCursor{}, err
	}
	if c.at, err = fromOptionalJSON(j.At); err != nil {
		return pageCursor{}, err
	}
	if len(j.Seen) > 0 {
		c.seen = make(map[logline.ContainerName]logline.RunPlace, len(j.Seen))
	}
	for _, e := range j.Seen {
		line, err := e.Cursor()
		if err != nil {
			return pageCursor{}, err
		}
		if line.ContainerName == (logline.ContainerName{}) {
			return pageCursor{}, errBadPageCursor
		}
		c.seen[line.ContainerName] = line.RunPlace
	}
	if err := c.check(); err != nil {
		return pageCursor{}, err
	}
	return c, nil
}

// fromOptionalJSON is the line cursor j spells, zero when j is nil.
func fromOptionalJSON(j *logline.CursorJSON) (logline.Cursor, error) {
	if j == nil {
		return logline.Cursor{}, nil
	}
	return j.Cursor()
}

// check refuses a decoded cursor no page writes: a count outside 0 to its Lines,
// a Start before an open Since with no count, or a window refused.
func (c pageCursor) check() error {
	countOK := func(n int, p Padding) bool { return n >= 0 && n <= p.Lines }
	switch {
	case c.ResolvedAt.IsZero(),
		!countOK(c.before, c.Window.Before), !countOK(c.after, c.Window.After),
		c.Since == nil && c.Window.Before.Lines == 0 && !c.Start.IsZero(),
		checkWindow(c.Window) != nil:
		return errBadPageCursor
	}
	return nil
}

// ===== The resolved window =====

// checkWindow refuses a window whose bounds cross, or whose padding is past its
// limits or names both a count and a span. Padding changes what the window means,
// so it is refused, never clamped.
func checkWindow(w Window) error {
	if w.Since != nil && w.Until != nil && w.Since.After(*w.Until) {
		return refusal.New(refusal.CodeInvalidRange, "since is after until")
	}
	for _, p := range []Padding{w.Before, w.After} {
		switch {
		case p.Lines != 0 && p.Span != 0:
			return refusal.New(refusal.CodeConflictingParameters, "padding is a number of lines or a span of time, not both")
		case p.Lines < 0 || p.Lines > MaxPageSize:
			return refusal.New(refusal.CodeBadPadding, fmt.Sprintf("padding is between 0 and %d lines", MaxPageSize))
		case p.Span < 0 || p.Span > MaxPaddingSpan:
			return refusal.New(refusal.CodeBadPadding, fmt.Sprintf("padding is a span between 0 and %s", MaxPaddingSpan))
		}
	}
	return nil
}

// resolvedWindow is a Window as the first page pinned it: the Window;
// ResolvedAt, the first page's clock, which an open Until's After is measured
// from on every page, so the window's end does not slide from page to page; and
// Start, the window's start as an inclusive position once it is known: zero for
// an open Since with no Before, the moment a time or a span names, and the
// farthest line a counted Before's read found. A stream no page has seen reads
// from it. Only a page cursor holds one.
type resolvedWindow struct {
	Window
	ResolvedAt time.Time
	Start      logline.Cursor
}

// side is which part of a window a line lies in: the range, or the padding
// before or after it.
type side int

const (
	sideRange side = iota
	sideBefore
	sideAfter
)

// side is the part of w rec lies in by its timestamp. Lines past an open Until
// are padding only when After is set; with none the range has no end.
func (w resolvedWindow) side(rec logline.Record) side {
	switch {
	case w.Since != nil && rec.Timestamp.Before(*w.Since):
		return sideBefore
	case w.After != (Padding{}) && w.rangeEnd().Above(rec):
		return sideAfter
	}
	return sideRange
}

// padding is s's padding.
func (w resolvedWindow) padding(s side) Padding {
	if s == sideBefore {
		return w.Before
	}
	return w.After
}

// counted is whether s's padding is a count of lines. Before an open Since there
// is no padding, so no count.
func (w resolvedWindow) counted(s side) bool {
	return w.padding(s).Lines > 0 && (s != sideBefore || w.Since != nil)
}

// lines is the count of s's padding.
func (w resolvedWindow) lines(s side) int { return w.padding(s).Lines }

// start is the window's start, inclusive, zero while it is open or a count's
// edge not yet found.
func (w resolvedWindow) start() logline.Bound {
	if w.Start.IsZero() {
		return logline.Bound{}
	}
	return logline.Bound{At: w.Start, Inclusive: true}
}

// end is the window's end, inclusive, zero while it is open or counted: Until, or
// for an open Until the first page's clock, widened by a span.
func (w resolvedWindow) end() logline.Bound {
	switch {
	case w.After.Lines > 0, w.Until == nil && w.After.Span == 0:
		return logline.Bound{}
	case w.Until == nil:
		return logline.Bound{At: logline.MomentAt(w.ResolvedAt.Add(w.After.Span)), Inclusive: true}
	}
	return logline.Bound{At: logline.MomentAt(w.Until.Add(w.After.Span)), Inclusive: true}
}

// rangeStart is the range's start, inclusive, zero when Since is open.
func (w resolvedWindow) rangeStart() logline.Bound {
	if w.Since == nil {
		return logline.Bound{}
	}
	return logline.Bound{At: logline.MomentAt(*w.Since), Inclusive: true}
}

// rangeEnd is the range's end, inclusive: Until, or with Until open and After set
// the nanosecond before the first page's clock, since After counts the lines at
// it and past it. Zero when the range has no end.
func (w resolvedWindow) rangeEnd() logline.Bound {
	switch {
	case w.Until != nil:
		return logline.Bound{At: logline.MomentAt(*w.Until), Inclusive: true}
	case w.After != (Padding{}):
		return logline.Bound{At: logline.MomentAt(w.ResolvedAt.Add(-time.Nanosecond)), Inclusive: true}
	}
	return logline.Bound{}
}

// inRange is whether a line stamped ts lies in the range: at or past Since and
// at or before rangeEnd, each when set. A line outside is padding.
func (w resolvedWindow) inRange(ts int64) bool {
	if w.Since != nil && ts < w.Since.UnixNano() {
		return false
	}
	end := w.rangeEnd()
	return end.At.IsZero() || ts <= end.At.Timestamp.UnixNano()
}

// afterIsEmpty is whether w pads an open Until and none of recs lies in that
// padding, at or past the first page's clock.
func (w resolvedWindow) afterIsEmpty(recs []logline.Record) bool {
	if w.Until != nil || w.After == (Padding{}) {
		return false
	}
	for _, rec := range recs {
		if !rec.Timestamp.Before(w.ResolvedAt) {
			return false
		}
	}
	return true
}

// ===== The streams a page reads =====

// maxInstances is the most of one pod's container's instances a page reads from the
// file beside what the resolve hands out: a pod that crash-looped for days can hold
// hundreds, each a walk of its own.
const maxInstances = 8

// pageStream is one stream a page reads: its file row, the stream the resolve
// handed out for it, nil for one only the file holds, its lines' stream and place as
// the page answers them, and the start of the tailer running on it, nil with none.
type pageStream struct {
	row   logstore.Stream
	st    *stream
	key   logline.StreamKey
	place logline.Place
	// tail is the start of the tailer running on the stream and committed how far it
	// had written when the page began, both nil with none.
	tail      *logstore.Pos
	committed *logstore.Pos
}

// exhausted is whether no backend serves ps: it is read from the file alone, and
// has nothing older than its oldest line and nothing newer than its newest.
func (ps pageStream) exhausted() bool { return ps.st == nil }

// run is ps's container and runtime id, at no moment: what orders its lines
// against another stream's at one moment.
func (ps pageStream) run() logline.Cursor {
	return logline.Cursor{ContainerName: logline.ContainerName{Namespace: ps.row.Namespace, Pod: ps.row.Pod, Container: ps.row.Container}, RunPlace: logline.RunPlace{ContainerID: ps.row.ContainerID}}
}

// pageStreams files t's streams in file, each as its instance's row, and answers
// them, then fileOnlyStreams. Every row is held from the moment its id is known
// until release, which the caller owes even on an error.
func (s *Service) pageStreams(ctx context.Context, file *logstore.Store, t target, reads []pageQuery) (_ []pageStream, release func(), err error) {
	rows, err := fileStreams(ctx, file, t.streams)
	if err != nil {
		return nil, func() {}, err
	}
	releaseRows := file.Hold(rowIDs(rows)...)
	out := make([]pageStream, len(t.streams))
	for i, st := range t.streams {
		out[i] = pageStream{row: rows[i], st: &t.streams[i], key: st.key, place: st.place}
	}
	only, releaseOnly, err := s.fileOnlyStreams(ctx, file, t, reads)
	release = func() {
		releaseRows()
		releaseOnly()
	}
	return append(out, only...), release, err
}

// fileOnlyStreams is each stream the file holds that reads, one scope's reads over
// one stream set, read beside the resolve's: a Pod source's instances the resolve
// handed out for no source, and a workload's gone pods, found by the chains filed
// with their streams, where a read reaches them. A pod is gone when the mirror no
// longer holds it, and the mirror is asked: the resolve hands out no stream of a
// live pod a previous view finds no previous instance of, and a workload's chain
// still names a pod it orphaned. Each instance once across the scope. Every row is
// held from the moment its id is known until release, which the caller owes even on
// an error.
func (s *Service) fileOnlyStreams(ctx context.Context, file *logstore.Store, t target, reads []pageQuery) (_ []pageStream, release func(), err error) {
	var releases []func()
	release = func() {
		for _, r := range releases {
			r()
		}
	}
	seen := map[logline.Cursor]bool{}
	resolvedPods := map[string]bool{}
	for _, st := range t.streams {
		seen[st.run()] = true
		resolvedPods[st.gen.uid] = true
	}
	var out []pageStream
	add := func(src ScopeSource, rows []logstore.Stream) {
		releases = append(releases, file.Hold(rowIDs(rows)...))
		for _, row := range fileRows(t, rows, src, reads[0].scope.Filters) {
			ps := fileStream(t, row)
			if !seen[ps.run()] {
				seen[ps.run()] = true
				out = append(out, ps)
			}
		}
	}
	scope := reads[0].scope
	var maybeGone []podRows
	for i, src := range scope.Sources {
		if src.Kind == SourcePod {
			held, err := file.PodStreams(ctx, src.Namespace, src.Name, t.pods[src.Namespace+"/"+src.Name])
			if err != nil {
				return nil, release, err
			}
			add(src, held)
			continue
		}
		if t.sourceUIDs[i] == "" {
			continue
		}
		owned, err := file.OwnedStreams(ctx, t.sourceUIDs[i])
		if err != nil {
			return nil, release, err
		}
		// A pod the resolve handed out is in the mirror, so the mirror is asked about
		// the rest alone, and about none on most pages.
		var rows []logstore.Stream
		for _, o := range owned {
			if !resolvedPods[o.PodUID] && reaches(reads, fileStream(t, o.Stream), o.Oldest, o.Newest) {
				rows = append(rows, o.Stream)
			}
		}
		maybeGone = append(maybeGone, podRows{src, rows})
	}
	gone, err := s.gonePods(ctx, t.cacheID, maybeGone)
	if err != nil {
		return nil, release, err
	}
	for _, c := range maybeGone {
		add(c.src, slices.DeleteFunc(c.rows, func(row logstore.Stream) bool { return !gone[row.PodUID] }))
	}
	return out, release, nil
}

// podRows is the file's rows a source would read were their pods gone.
type podRows struct {
	src  ScopeSource
	rows []logstore.Stream
}

// gonePods is the UID of each pod of candidates the mirror behind cacheID no longer
// holds. Unsure, podsGone answers none, and a live pod read as gone would serve a
// current instance as previous, so none is the safe answer.
func (s *Service) gonePods(ctx context.Context, cacheID cluster.ClusterCacheID, candidates []podRows) (map[string]bool, error) {
	uids := map[string]bool{}
	for _, c := range candidates {
		for _, row := range c.rows {
			uids[row.PodUID] = true
		}
	}
	if len(uids) == 0 {
		return nil, nil
	}
	gone, err := s.podsGone(ctx, int64(cacheID), slices.Sorted(maps.Keys(uids)))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, uid := range gone {
		out[uid] = true
	}
	return out, nil
}

// reaches is whether ps, its lines running from oldest to newest, has a line one of
// reads can deliver: at or past where ps's own read starts, since on a continuation
// each stream goes on from its own entry and one with none from the window's edge,
// and short of the read's far edge.
func reaches(reads []pageQuery, ps pageStream, oldest, newest logstore.Pos) bool {
	for _, q := range reads {
		first, last := oldest, newest
		if q.mode == logline.Older {
			first, last = newest, oldest
		}
		far := q.far(ps)
		if onward(q.mode, last, q.key(ps)) && (far == nil || onward(q.mode, *far, first)) {
			return true
		}
	}
	return false
}

// onward is whether a is at or past b, read mode.
func onward(mode logline.Direction, a, b logstore.Pos) bool {
	if mode == logline.Newer {
		return a.Compare(b) >= 0
	}
	return a.Compare(b) <= 0
}

// podOf is what the mirror holds of row's pod: nothing once the pod is gone, though a
// later pod of its name runs.
func podOf(t target, row logstore.Stream) podNow {
	key := row.Namespace + "/" + row.Pod
	live := row.PodUID != "" && t.pods[key] == row.PodUID
	return podNow{live: live, def: t.defaults[key], place: t.places[key]}
}

// fileStream is row as a page reads it from the file: previous unless it is its
// container's current instance.
func fileStream(t target, row logstore.Stream) pageStream {
	current := t.current[containerKey(row.Namespace, row.Pod, row.Container)]
	return pageStream{
		row:   row,
		key:   logline.StreamKey{Namespace: row.Namespace, Pod: row.Pod, Container: row.Container, Previous: row.ContainerID != current},
		place: podOf(t, row).placeOf(row),
	}
}

// rowIDs is the id of each of rows.
func rowIDs(rows []logstore.Stream) []int64 {
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

// fileStreams files streams in file, each as its instance's row with its pod's
// controller chain, and answers the rows with their ids, in order. A restart the
// mirror reported late may have filed the next instance's lines under the one before,
// so the file's row of that one is trimmed once its end is known.
func fileStreams(ctx context.Context, file *logstore.Store, streams []stream) ([]logstore.Stream, error) {
	rows := make([]logstore.Stream, len(streams))
	for i, st := range streams {
		rows[i] = logstore.Stream{
			Namespace: st.key.Namespace, Pod: st.key.Pod, PodUID: st.gen.uid,
			Container: st.key.Container, Default: st.isDefault, ContainerID: st.gen.instance,
			StartedAt: st.startedAt, FinishedAt: st.finishedAt,
			Node: st.place.Node, Region: st.place.Region, Zone: st.place.Zone, OS: st.place.OS, Arch: st.place.Arch,
			Labels: st.labels, Owners: st.owners,
		}
	}
	ids, err := file.PutStreams(ctx, rows)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].ID = ids[i]
	}
	var finished []logstore.Stream
	for _, st := range streams {
		if st.previous != "" && st.previousFinishedAt != 0 {
			finished = append(finished, logstore.Stream{
				Namespace: st.key.Namespace, Pod: st.key.Pod, Container: st.key.Container,
				ContainerID: st.previous, FinishedAt: st.previousFinishedAt,
			})
		}
	}
	if len(finished) > 0 {
		if err := file.FinishStreams(ctx, finished); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// podNow is what the mirror says of a Pod source's pod now: whether it holds the pod,
// and if so its default container and where it runs. A gone pod's rows keep what was
// filed of them.
type podNow struct {
	live  bool
	def   string
	place logline.Place
}

// placeOf is where row's instance ran: where the pod runs now while the mirror holds
// it, since a node's labels can move, else where it was filed.
func (p podNow) placeOf(row logstore.Stream) logline.Place {
	if p.live {
		return p.place
	}
	return logline.Place{Node: row.Node, Region: row.Region, Zone: row.Zone, OS: row.OS, Arch: row.Arch}
}

// isDefault is whether row's container is the pod's default: the one the mirror names
// while it holds the pod, since its annotation can move, else the rows marked so.
func (p podNow) isDefault(row logstore.Stream) bool {
	if p.live {
		return row.Container == p.def
	}
	return row.Default
}

// fileRows is the rows of held, a pod's streams in the file newest first per
// container, that src reads: the containers it names, every one, or the pod's
// default, as the resolve names them; with Previous, not the pod's current instance;
// in a place the filters keep; and each container's newest maxInstances, per pod.
func fileRows(t target, held []logstore.Stream, src ScopeSource, filters []ScopeFilter) []logstore.Stream {
	var out []logstore.Stream
	count := map[string]int{}
	for _, row := range held {
		pod, instances := podOf(t, row), row.PodUID+"/"+row.Container
		switch {
		case len(src.Containers) > 0 && !slices.Contains(src.Containers, row.Container),
			len(src.Containers) == 0 && !src.AllContainers && !pod.isDefault(row),
			src.Previous && row.ContainerID == t.current[containerKey(row.Namespace, row.Pod, row.Container)],
			!keptByFilters(pod.placeOf(row), filters),
			count[instances] == maxInstances:
			continue
		}
		count[instances]++
		out = append(out, row)
	}
	return out
}

// keptByFilters is whether every filter keeps an instance that ran in place.
func keptByFilters(place logline.Place, filters []ScopeFilter) bool {
	for _, f := range filters {
		var value string
		switch f.Field {
		case FilterNode:
			value = place.Node
		case FilterRegion:
			value = place.Region
		case FilterZone:
			value = place.Zone
		case FilterOS:
			value = place.OS
		case FilterArch:
			value = place.Arch
		}
		if !slices.Contains(f.Values, value) {
			return false
		}
	}
	return true
}

// ===== The filler =====

// errRestarted is a stream whose pod ran another instance by the time its reads
// ended, so what they read was thrown away.
var errRestarted = errors.New("restarted while reading")

// streamFill is what a page's fills did for one stream: the failure that skips the
// stream, and what the page and the walk need to know of the reads.
type streamFill struct {
	err error
	// retired is whether the connection's retirement cut a read before its first
	// line, which refuses the page as the merge refuses a live read so cut.
	retired bool
	// fromKey is whether the first gap a fill may be offered was read from where the
	// walk starts in it, the page's bound or the tailer's start, so nothing lies
	// between there and the first line past it: the walk crosses that gap. toEnd is
	// whether a newer page read the newest gap it may fill whole, to the stream's end
	// or to the tailer's start, which no chunk sits at: the walk crosses it.
	fromKey bool
	toEnd   bool
	// toFar is whether the fill read the gap the page's far bound lies in up to the
	// bound, so the walk crosses it there.
	toFar bool
	// wrote is whether a write filed anything, and gone whether one found the stream's
	// row gone: the janitor took a gone pod's row with nothing in the file before the
	// page held it, and the page reads it as a stream no backend serves.
	wrote bool
	gone  bool
	// up is what an older fill from the same bound links its first window up to: the
	// first line a newer fill read from its bound, or, when it read its first gap whole
	// and found none, that gap's far edge. Nil with neither.
	up *logstore.Pos
	// stop is the moment a read was cut or stopped at when what it wrote does not
	// show it: an empty window's start, or a loud second's edge. The walk that
	// reaches the gap the read ended in stops there. Zero with none.
	stop logline.Cursor
	// downloaded is the bytes the stream's reads downloaded, which its walk may read
	// past its share.
	downloaded int64
}

// filler fills one stream for one page: the gaps in the page's direction from its
// bound go to the cluster's backends in order, until the stream holds need lines
// the page keeps past the bound, has no live gap left that way, or a read of it
// was cut. A read's lines are written a batch at a time as they arrive.
type filler struct {
	file *logstore.Store
	// clusters and id are the mirror each write checks the stream's instance against,
	// and save writes one batch's fills: a page's stamped, a tail's held.
	clusters cluster.Service
	id       cluster.ClusterID
	// serverUID pins the mirror's checks to the page's one server, "" for any: a
	// move to another is a mismatch, never a pod restart.
	serverUID string
	save      func(context.Context, []logstore.Fill) (logstore.Written, error)
	// ret sizes the batches a write files.
	ret      logstore.Retention
	ps       pageStream
	backends []kubelet.Backend
	names    []string
	q        pageQuery
	// limits is the stream's account of the query, which every read of it spends.
	limits *kubelet.Limits
	// walk is the stream's share of the walks' budget, which counting what the file
	// holds spends.
	walk *budget
	// held is whether a chunk of the page's pattern claims a line, as the walk keeps
	// only such lines.
	held func(logstore.Pos) bool
	// seam is the stream's line just past an older page's bound, which a newer fill from
	// the same bound proved nothing lies before: the read back from the bound links
	// up to it. Nil for none.
	seam *logstore.Pos
	// near is the rate next to the point of the gap an older fill reads, nil with none
	// measured.
	near *kubelet.Rate
	// far is where the page's range ends in the stream, nil for no end.
	far *logstore.Pos
	// count is the lines the stream holds for the page, past of them past the
	// range's end.
	count, past int
	out         streamFill
}

// run fills the stream from key, its bound, and answers what it did. Only a fatal
// failure or one of the file is an error.
func (f *filler) run(ctx context.Context, key logstore.Pos) (streamFill, error) {
	chunks, err := f.file.Chunks(ctx, f.ps.row.ID, f.q.scope.Grep)
	if err != nil {
		return streamFill{}, err
	}
	f.held = heldBy(chunks)
	segs := segmentsFrom(logstore.CoverageOf(chunks, f.names, f.ps.tail), key, f.q.mode)
	// Counting a span only decides whether to fill a gap past it, so the fill ends
	// at the last live gap.
	segs = segs[:lastLiveGap(segs)+1]
	f.far = f.q.far(f.ps)
	first := true
	for _, seg := range segs {
		if f.far != nil && pastFar(seg, *f.far, f.q.mode) {
			break
		}
		var more bool
		switch {
		case seg.span != nil:
			more, err = f.countSpan(ctx, *seg.span, key)
		case seg.gap.Live():
			more, err = f.fillGap(ctx, *seg.gap, key, first)
		default:
			continue
		}
		if err != nil || !more {
			return f.out, err
		}
		first = false
	}
	return f.out, nil
}

// lastLiveGap is the index of the last gap of segs that a fill may be offered, -1
// with none.
func lastLiveGap(segs []segment) int {
	for i, seg := range slices.Backward(segs) {
		if seg.gap != nil && seg.gap.Live() {
			return i
		}
	}
	return -1
}

// filled is whether the stream holds what the page needs past its bound.
func (f *filler) filled() bool { return f.q.full(f.count, f.past) }

// countSpan counts the lines past key in sp that the walk keeps, and is whether the
// fill may go on past the span: the stream still needs lines, and the walks' budget
// is not spent.
func (f *filler) countSpan(ctx context.Context, sp logstore.Span, key logstore.Pos) (bool, error) {
	from, to := logstore.MaxPos(key, sp.From.Prev()), sp.To
	if f.q.mode == logline.Older {
		from, to = logstore.MinPos(key, sp.To.Next()), sp.From
	}
	counted := true
	err := f.file.Read(ctx, func(r logstore.Reader) error {
		return r.Lines(f.ps.row.ID, from, to, func(l logstore.Line) bool {
			if !f.walk.spend(len(l.Message)) {
				counted = false
				return false
			}
			if f.held(l.Pos) && f.q.keep(l.TS, l.Message) {
				f.count++
				if f.q.pastEnd(l.Pos) {
					f.past++
				}
			}
			return !f.filled()
		})
	})
	return counted && !f.filled(), err
}

// fillGap reads g, a live gap, from the backends, and is whether the fill may go on
// past it: the gap was read whole, so no limit cut it, the stream still needs lines,
// the read did not fail, and the stream's limits are not used up. first is whether g
// is the first gap the fill is offered, the one the walk starts in. The fill ends at
// the page's far bound.
func (f *filler) fillGap(ctx context.Context, g logstore.Gap, key logstore.Pos, first bool) (bool, error) {
	lower, upper, prev, next := gapRange(g, key, f.q.mode)
	if f.q.mode == logline.Older && upper != nil && next == nil {
		// The read starts at the bound, inside the gap.
		next = f.seam
	}
	near, err := f.rateAbove(ctx, g.Before)
	if err != nil {
		return false, err
	}
	f.near = near
	// A gap the far bound lies in is read up to the bound, where no chunk sits.
	clamped := false
	switch {
	case f.far == nil:
	case f.q.mode == logline.Newer && (upper == nil || upper.Compare(*f.far) > 0):
		upper, next, clamped = f.far, nil, true
	case f.q.mode == logline.Older && (lower == nil || lower.Compare(*f.far) < 0):
		lower, prev, clamped = f.far, nil, true
	}
	whole, oldest, err := f.read(ctx, g.Tier, lower, upper, prev, next)
	if err != nil {
		return false, err
	}
	if first && (whole || oldest != nil) {
		f.out.fromKey = true
		if f.q.mode == logline.Newer {
			f.out.up = oldest
			if oldest == nil {
				f.out.up = upper
			}
		}
	}
	if clamped {
		f.out.toFar = whole
		return false, nil
	}
	if whole && f.q.mode == logline.Newer && g.Newest() {
		f.out.toEnd = true
	}
	return whole && !f.filled() && f.out.err == nil && !f.limits.UsedUp(), nil
}

// rateAbove is the rate of an older fill's lines next to its point, sampled oldest
// first from above, the first line of the chunk the gap ends at. It is nil for a
// newer fill, a gap that ends at nothing, or too few lines to measure. The point
// often lies inside the gap, so the chunk is the gap's, not one at the point.
func (f *filler) rateAbove(ctx context.Context, above *logstore.Pos) (*kubelet.Rate, error) {
	if f.q.mode != logline.Older || above == nil {
		return nil, nil
	}
	need := int64(f.q.need())
	var s kubelet.Sample
	err := f.file.Read(ctx, func(r logstore.Reader) error {
		s = kubelet.Sample{}
		return r.Lines(f.ps.row.ID, above.Prev(), logstore.Newest, func(l logstore.Line) bool {
			ts := time.Unix(0, l.TS)
			s.Add(ts, kubelet.WireBytes(ts, l.Message), logline.Matches(f.q.grep, l.Message))
			return s.Short(need)
		})
	})
	near, ok := s.Rate()
	if err != nil || !ok {
		return nil, err
	}
	return &near, nil
}

// read reads the range between lower and upper, both excluded and nil for none, from
// the backend at tier, and writes what it read and proved. prev and next are the
// chunk edges the range runs from and to, nil where it does not reach one. It
// answers whether the read served every line in the range, and the oldest line it
// wrote, nil with none. A read that reaches its backend's beginning offers what is
// older than its first line to the next.
func (f *filler) read(ctx context.Context, tier int, lower, upper, prev, next *logstore.Pos) (whole bool, oldest *logstore.Pos, err error) {
	p, ok, err := f.pull(ctx, tier, lower, upper, prev, next)
	if !ok || err != nil {
		return false, nil, err
	}
	if !p.whole && p.r.Short && p.r.Stop.IsMoment() {
		f.out.stop = p.r.Stop
	}
	if !p.whole && kubelet.FailureKind(p.r.Err) == kubelet.FailLimit {
		// The stream stopped at a loud second: the page names it, and is cut there. A
		// read whole stopped past its range, which a later gap meets again.
		f.out.err = p.r.Err
	}
	got := p.oldest != nil
	if !got && !p.whole {
		// Nothing in range was read: the stream is skipped, the page refused when the
		// retirement cut the read, or cut at the read's moment.
		switch {
		case p.r.Short && p.r.Stop.IsZero():
			f.out.retired = errors.Is(p.r.Err, kubelet.ErrRetired)
		case !p.r.Short:
			f.out.err = p.r.Err
		}
		return false, nil, nil
	}
	if ok, err := f.link(ctx, tier, p, prev, next); !ok {
		return false, nil, err
	}
	if p.offer {
		// The moment of the backend's first line is its whole: the next is asked for
		// what is older than that moment.
		older, olderNext := upper, next
		if got {
			older, olderNext = &logstore.Pos{TS: p.oldest.TS, Ord: -1}, p.oldest
		}
		olderWhole, olderOldest, err := f.read(ctx, tier+1, lower, older, prev, olderNext)
		if olderOldest != nil {
			return p.whole && olderWhole, olderOldest, err
		}
		return p.whole && olderWhole, p.oldest, err
	}
	return p.whole, p.oldest, nil
}

// pulled is one backend read as pull ended it: the read; whether it served every
// line in the range; oldest, a forward read's first line or the first line of the
// last window a backward read completed, nil with none; atBeginning, whether the
// read reached the backend's oldest line; and offer, whether what lies below its first
// line goes to the next backend.
type pulled struct {
	r                  *kubelet.Read
	whole              bool
	oldest             *logstore.Pos
	atBeginning, offer bool
}

// up is what a window links up to: the oldest line the read has written, or next,
// the chunk at the point, while it has written none. An empty window writes nothing,
// so the next older one links over it.
func (p pulled) up(next *logstore.Pos) *logstore.Pos {
	if p.oldest != nil {
		return p.oldest
	}
	return next
}

// pull runs the backend read of the range and writes its lines as they arrive, a
// batch at a time, counting the lines the page keeps. A forward read is one
// window whose lines arrive nearest the point first: its first batch links down,
// and it is cancelled once the stream holds what the page needs. A backward read's
// windows arrive newest first, each oldest first: a window links up only at its
// end, and counts toward the page only then; one stopped part way, or one the read
// ended inside, is an island linked to nothing. The read is stopped between
// windows, since the lines a window has counted part way are the ones farthest
// from the point. It is false once a write ended the read. A read the page's
// context ended is an error.
func (f *filler) pull(ctx context.Context, tier int, lower, upper, prev, next *logstore.Pos) (pulled, bool, error) {
	forward := f.q.mode == logline.Newer
	o := kubelet.Options{Since: f.boundAt(lower), Until: f.boundAt(upper), Limits: f.limits, Batch: int64(f.q.need()), Keep: f.q.kept, Near: f.near}
	// began is the moment a forward read's GET began at, told before its first line
	// arrives.
	var began time.Time
	o.OnOldest = func(t time.Time) { began = t }
	// answers holds the answer to the reader's next more, whether another window
	// may follow, given once a window's last batch is written. The reader asks only
	// when another GET would follow, so an answer it never took is replaced by the
	// next window's. stopped is the reader taking a no.
	answers := make(chan bool, 1)
	stopped := false
	o.More = func(ctx context.Context) bool {
		select {
		case ok := <-answers:
			stopped = !ok
			return ok
		case <-ctx.Done():
			return false
		}
	}
	var r *kubelet.Read
	if forward {
		r = f.backends[tier].Forward(ctx, f.ps.st.log(), o)
	} else {
		r = f.backends[tier].Backward(ctx, f.ps.st.log(), o)
	}
	out := pulled{r: r}
	w := f.newWindow(tier)
	abandon := func(err error) (pulled, bool, error) {
		r.Cancel()
		drainReading(r)
		return pulled{}, false, err
	}
	cut := false
	for it := range r.Lines.Chan() {
		if it.WindowCut {
			// The window was stopped part way, so the lines between it and the point
			// were never read: an island, linked to nothing, whose kept lines the page
			// cannot reach.
			if ok, err := f.endWindow(ctx, w, nil, false); !ok {
				return abandon(err)
			}
			w = f.newWindow(tier)
			continue
		}
		if it.WindowEnd {
			// The window was read whole, so its last batch links it up. The read's
			// first window from the end of a finished instance holds its last line.
			atEnd := upper == nil && out.oldest == nil && f.ps.st.finished()
			if ok, err := f.endWindow(ctx, w, out.up(next), atEnd); !ok {
				return abandon(err)
			}
			if w.first != nil {
				out.oldest = w.first
			}
			f.count += w.kept
			w = f.newWindow(tier)
			select {
			case <-answers:
			default:
			}
			answers <- !f.filled()
			continue
		}
		if forward && w.first == nil {
			// A forward read links down from its first batch, so what it continues
			// is judged at its first line, from the moment its GET began at.
			out.atBeginning = f.reachedBeginning(lower, prev, began, true)
			w.batch.AtBeginning = out.atBeginning
			if !out.atBeginning {
				w.batch.Prev, w.batch.Merge = prev, returnedWhole(began, prev)
			}
		}
		if ok, err := f.add(ctx, w, lineOf(it.Record)); !ok {
			return abandon(err)
		}
		// A forward read counts as its lines arrive, nearest the point first; a
		// backward window counts once it completes, since its lines arrive farthest
		// first.
		if f.q.kept(it.Record) {
			if forward {
				f.count++
				if f.q.pastEnd(lineOf(it.Record).Pos) {
					f.past++
				}
			} else {
				w.kept++
			}
		}
		if forward && f.filled() {
			cut = true
			r.Cancel()
			drainReading(r)
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return pulled{}, false, err
	}
	if err := kubelet.FatalError(r.Err); err != nil {
		return pulled{}, false, err
	}
	clean := r.Err == nil && !r.Short
	// A forward GET reads from the range's lower second, so one whose first line lies
	// past the range found the backend holds none of it, however the read then ended.
	past := forward && w.first == nil && upper != nil && !began.IsZero() && began.UnixNano() > upper.TS
	out.whole = !cut && !stopped && (clean || past)
	if forward {
		out.oldest = w.first
		if w.first == nil {
			out.atBeginning = f.reachedBeginning(lower, prev, began, !began.IsZero() || clean)
		}
		out.offer = tier+1 < len(f.backends) && out.atBeginning
		// The read's last batch links up only when the read served every line to next.
		up := next
		if !out.whole {
			up = nil
		}
		if ok, err := f.endWindow(ctx, w, up, r.AtEnd); !ok {
			return pulled{}, false, err
		}
		return out, true, nil
	}
	// A window the read ended inside is an island: what it read goes to the file,
	// linked up to nothing, since the lines between it and the point were never read.
	if ok, err := f.endWindow(ctx, w, nil, false); !ok {
		return pulled{}, false, err
	}
	out.atBeginning = r.AtBeginning
	out.offer = tier+1 < len(f.backends) && (r.AtBeginning || hole(out.oldest, out.whole, prev))
	return out, true, nil
}

// reachedBeginning is whether a forward read from lower reached the backend's
// oldest line: it has no lower bound, so it started there, or it read from prev's second and its
// GET began past prev's moment, so prev's line rotated away. began is the moment of
// the GET's first line, zero with none, and proved whether the read got a line or
// ended clean: a GET that failed before its first line says nothing. A read from any
// other bound, a page's key, says nothing of rotation.
func (f *filler) reachedBeginning(lower, prev *logstore.Pos, began time.Time, proved bool) bool {
	fromPrev := prev != nil && lower != nil && *lower == *prev
	return proved && (lower == nil || fromPrev && rotatedAway(began, prev))
}

// hole is whether a backward read from prev left a range below its oldest line it
// may not have served: it reached back to prev with its oldest line past it, or
// found nothing at all in a range it read whole. A read that stopped short of prev,
// cut by the page filling or by a limit, says nothing of what lies below it.
func hole(oldest *logstore.Pos, whole bool, prev *logstore.Pos) bool {
	if prev == nil || !whole {
		return false
	}
	return oldest == nil || oldest.TS > prev.TS
}

// link writes what a read proved beyond its lines, once it has ended, and is false
// once the write ended the read. A forward read links down from its first batch and
// up from its last, so only one that wrote no line has links left: from prev to
// next, with its marks. A backward read links up window by window and down once it
// ends: from the oldest line it wrote, or next with none, to prev, which only a read
// that served every line down to prev proves, with the beginning mark.
func (f *filler) link(ctx context.Context, tier int, p pulled, prev, next *logstore.Pos) (bool, error) {
	fill := logstore.Fill{Stream: f.ps.row.ID, Backend: f.names[tier], AtBeginning: p.atBeginning}
	if f.q.mode == logline.Newer {
		if p.oldest != nil {
			return true, nil
		}
		if !p.atBeginning {
			fill.Prev = prev
		}
		if p.whole {
			fill.Next = next
		}
		fill.AtEnd = p.r.AtEnd
		return f.write(ctx, fill)
	}
	fill.Next = p.up(next)
	if p.whole && !p.atBeginning && !p.offer {
		fill.Prev = prev
	}
	if fill.Prev == nil && !fill.AtBeginning {
		return true, nil
	}
	return f.write(ctx, fill)
}

// window is one window of a read as it arrives, written a batch at a time. batch is
// the fill of the lines not yet written: the window's first carries its link down and
// beginning mark, each later one continues the chunk the one before it wrote. first is the
// window's first line and last the last line written, nil before either.
type window struct {
	batch       logstore.Fill
	bytes       int64
	first, last *logstore.Pos
	// kept is a backward window's lines the page keeps, counted toward the page
	// once it completes.
	kept int
}

// newWindow is an empty window of the backend at tier's read.
func (f *filler) newWindow(tier int) *window {
	return &window{batch: logstore.Fill{Stream: f.ps.row.ID, Backend: f.names[tier]}}
}

// add takes line into w, first writing the batch it fills, and is false once a
// write ended the read.
func (f *filler) add(ctx context.Context, w *window, line logstore.Line) (bool, error) {
	if n := len(w.batch.Lines); n > 0 && (n >= f.ret.WriteBatch || w.bytes >= f.ret.WriteBatchBytes) {
		if ok, err := f.flush(ctx, w); !ok {
			return false, err
		}
	}
	if w.first == nil {
		w.first = new(line.Pos)
	}
	w.batch.Lines = append(w.batch.Lines, line)
	w.bytes += int64(len(line.Message))
	return true, nil
}

// endWindow writes w's last batch, linked up to next and marked atEnd, and is false
// once the write ended the read. A window with no line writes nothing.
func (f *filler) endWindow(ctx context.Context, w *window, next *logstore.Pos, atEnd bool) (bool, error) {
	if len(w.batch.Lines) == 0 {
		return true, nil
	}
	w.batch.Next, w.batch.AtEnd = next, atEnd
	return f.flush(ctx, w)
}

// flush writes w's batch, and starts the next one as the continuation of the chunk
// it ends.
func (f *filler) flush(ctx context.Context, w *window) (bool, error) {
	if ok, err := f.write(ctx, w.batch); !ok {
		return false, err
	}
	// A copy, so the batch's lines are not kept alive by it.
	w.last = new(w.batch.Lines[len(w.batch.Lines)-1].Pos)
	w.batch = logstore.Fill{Stream: w.batch.Stream, Backend: w.batch.Backend, Prev: w.last, Merge: true}
	w.bytes = 0
	return true, nil
}

// write files one batch's fill once the mirror shows the stream's pod still runs the
// instance it was resolved as, and is whether the read may go on. The mirror lags a
// restart, while the kubelet serves whichever instance runs now: a stream that
// restarted is skipped, and what it wrote before stays.
func (f *filler) write(ctx context.Context, fill logstore.Fill) (bool, error) {
	serves, err := f.serves(ctx)
	if err != nil {
		return false, err
	}
	if !serves {
		f.out = streamFill{err: errRestarted, wrote: f.out.wrote}
		return false, nil
	}
	w, err := f.save(ctx, []logstore.Fill{fill})
	if err != nil {
		return false, err
	}
	if len(w.Skipped) > 0 {
		f.out.gone = true
		return false, nil
	}
	f.out.wrote = true
	return true, nil
}

// serves is whether the mirror shows the stream's pod running the instance it was
// resolved as.
func (f *filler) serves(ctx context.Context) (bool, error) {
	var serves bool
	err := f.clusters.CachedData().ReadActive(ctx, f.id, f.serverUID, maxRows, maxBytes, func(q cluster.CacheReading, _ cluster.ActiveCluster) error {
		var err error
		serves, err = reading{q}.serves(*f.ps.st)
		return err
	})
	if err != nil {
		return false, mirrorErr(err)
	}
	return serves, nil
}

// rotatedAway is whether a forward read from prev's second, a fill's or a tailer's,
// whose GET's first line came at oldest, found prev's line gone. Rotation drops the
// oldest lines first, so everything up to prev is gone too, and the GET began at the
// backend's oldest line: the read is at the beginning.
func rotatedAway(oldest time.Time, prev *logstore.Pos) bool {
	return prev != nil && !returnedWhole(oldest, prev)
}

// returnedWhole is whether a forward read from prev's second, whose GET's first line
// came at oldest, returned prev's moment whole, so it extends the chunk ending there.
func returnedWhole(oldest time.Time, prev *logstore.Pos) bool {
	return prev != nil && !oldest.IsZero() && oldest.UnixNano() <= prev.TS
}

// boundAt is the read's bound at p, a position of the stream, excluded; zero with
// none.
func (f *filler) boundAt(p *logstore.Pos) logline.Bound {
	if p == nil {
		return logline.Bound{}
	}
	return logline.Bound{At: f.ps.run().At(*p), Inclusive: false}
}

// segment is one span or one gap of a stream's coverage.
type segment struct {
	span *logstore.Span
	gap  *logstore.Gap
}

// segmentsFrom is cov's spans and gaps in the order a page reading mode from key
// meets them, less the ones wholly behind key: a span, or a gap with no place left
// for a line between key and its far edge.
func segmentsFrom(cov logstore.Coverage, key logstore.Pos, mode logline.Direction) []segment {
	var all []segment
	spans := cov.Spans
	for i := range cov.Gaps {
		// A span goes before the first gap that starts at or past its end.
		g := &cov.Gaps[i]
		for len(spans) > 0 && g.After != nil && spans[0].To.Compare(*g.After) <= 0 {
			all = append(all, segment{span: &spans[0]})
			spans = spans[1:]
		}
		all = append(all, segment{gap: g})
	}
	newer := mode == logline.Newer
	if !newer {
		slices.Reverse(all)
	}
	var out []segment
	for _, seg := range all {
		var behind bool
		switch {
		case seg.span != nil && newer:
			behind = seg.span.To.Compare(key) <= 0
		case seg.span != nil:
			behind = seg.span.From.Compare(key) >= 0
		case newer:
			behind = seg.gap.Before != nil && seg.gap.Before.Prev().Compare(key) <= 0
		default:
			behind = seg.gap.After != nil && seg.gap.After.Next().Compare(key) >= 0
		}
		if !behind {
			out = append(out, seg)
		}
	}
	return out
}

// gapRange is the part of g a page reading mode from key reads: its edges, nil for
// none, and the chunk edges among them, which a fill links to. Where key lies inside
// g, the range starts at key and links to nothing there.
func gapRange(g logstore.Gap, key logstore.Pos, mode logline.Direction) (lower, upper, prev, next *logstore.Pos) {
	lower, upper, prev, next = g.After, g.Before, g.After, g.Before
	if mode == logline.Newer && key != logstore.Oldest && (g.After == nil || key.Compare(*g.After) > 0) {
		lower, prev = &key, nil
	}
	if mode == logline.Older && key != logstore.Newest && (g.Before == nil || key.Compare(*g.Before) < 0) {
		upper, next = &key, nil
	}
	return lower, upper, prev, next
}

// ===== The walker =====

// maxWalkBudget is the bytes one page's walks of the file read, shared evenly across
// its streams; each stream's walk also reads what its fill downloaded, so a stream
// read far is walked as far.
const maxWalkBudget = 64 << 20

// budget is the bytes a walk of the file may still read. A nil budget is no bound.
type budget struct{ left atomic.Int64 }

func newBudget(n int64) *budget {
	b := &budget{}
	b.left.Store(n)
	return b
}

// spend takes n bytes, and is false once there were not n left.
func (b *budget) spend(n int) bool { return b == nil || b.left.Add(-int64(n)) >= 0 }

// walker walks one stream of one page's file: its lines from the page's bound in the
// page's direction, bounded by the span the bound sits in with lost gaps crossed,
// that the page keeps, until it holds need of them or has spent its budget.
type walker struct {
	r      logstore.Reader
	ps     pageStream
	names  []string
	q      pageQuery
	budget *budget
	fill   streamFill
	// look is what the look past the walk's edge may read.
	look int64
}

// walked is one stream's walk: the reading the merge takes, its lines in the order
// the page reads them, and whether the walk stopped at a live gap of a stream skipped on a transient
// failure, which does not cut the page but leaves more to read that way. A stream
// the server denied or answered for has nothing more to give the next page, and
// neither does one that restarted, since the next resolve reads the new instance.
// read is the chunks the walk read a line from, which the page stamps, and reach
// how far it read.
type walked struct {
	reading *kubelet.Read
	left    bool
	read    []logstore.Chunk
	reach   streamReach
}

// streamReach is how far one stream's walk read, in the page's direction: the
// stream's run, its first and last rows, kept or rejected by the grep, zero with
// none, and the stream's own moment its read stopped at, zero when it stopped at
// none. A page cursor's entries are computed from it.
type streamReach struct {
	run         logline.Cursor
	first, last logline.Cursor
	stop        logline.Cursor
}

// walk reads the stream from key. A walk stopped by its budget, or by a live gap on
// a stream not skipped or stopped at a loud second, stops short where it reached, so
// the page is cut there: at the fill's stop when that lies past what it read.
func (w walker) walk(key logstore.Pos) (walked, error) {
	chunks, err := w.r.Chunks(w.ps.row.ID, w.q.scope.Grep)
	if err != nil {
		return walked{}, err
	}
	cov := logstore.CoverageOf(chunks, w.names, w.ps.tail)
	reach, gap := w.reach(cov, key)
	held := heldBy(chunks)
	var recs []logline.Record
	var first, last logstore.Pos
	read := false
	cut := w.fill.err == nil || kubelet.FailureKind(w.fill.err) == kubelet.FailLimit
	short := gap && cut
	spent := false
	past := 0
	err = w.r.Lines(w.ps.row.ID, key, reach, func(l logstore.Line) bool {
		if !read {
			first = l.Pos
		}
		last, read = l.Pos, true
		spent = !w.budget.spend(len(l.Message))
		if held(l.Pos) && w.q.keep(l.TS, l.Message) {
			recs = append(recs, w.record(l))
			if w.q.pastEnd(l.Pos) {
				past++
			}
		}
		switch {
		case spent:
			short = true
			return false
		case w.q.full(len(recs), past):
			short = false
			return false
		}
		return true
	})
	if err != nil {
		return walked{}, err
	}
	if err := w.mark(recs, cov, held); err != nil { // coverage:ignore only the file failing between the walk and its look back, in one read transaction
		return walked{}, err
	}
	var stop logline.Cursor
	var failed error
	switch {
	case read:
		stop = w.ps.run().At(last)
	case short && w.fill.retired:
		failed = kubelet.ErrRetired
	}
	// At the gap the fill ended in, the fill's stop is the edge when it lies past what
	// the walk read: an empty window's start, or a loud second.
	reached := streamReach{run: w.ps.run()}
	if short && !spent && !w.fill.stop.IsZero() && (stop.IsZero() || beyond(w.q.mode, w.fill.stop, stop)) {
		stop = w.fill.stop
		reached.stop = logline.Cursor{ContainerName: reached.run.ContainerName, RunPlace: logline.RunPlace{Timestamp: stop.Timestamp}}
	}
	transient := kubelet.FailureKind(w.fill.err) == kubelet.FailTransient && !errors.Is(w.fill.err, errRestarted)
	left := gap && transient && !w.q.full(len(recs), past)
	out := walked{reading: readingOf(w.ps.key, recs, short, stop, failed), left: left}
	if read {
		out.read = readFrom(chunks, first, last)
		reached.first, reached.last = reached.run.At(first), reached.run.At(last)
	}
	out.reach = reached
	return out, nil
}

// readFrom is each of chunks holding a line between a and b, either way round.
func readFrom(chunks []logstore.Chunk, a, b logstore.Pos) []logstore.Chunk {
	if a.Compare(b) > 0 {
		a, b = b, a
	}
	var out []logstore.Chunk
	for _, c := range chunks {
		if c.From.Compare(b) <= 0 && c.To.Compare(a) >= 0 {
			out = append(out, c)
		}
	}
	return out
}

// reach is how far from key the walk may read: through the spans, across the gaps no
// fill may be offered, the first live gap when the fill read it from where the walk
// starts, the newest when the fill read it to its end, and the one the far bound
// lies in when the fill read it to the bound, up to the first live gap left or the
// far bound, and whether it stops at a gap.
func (w walker) reach(cov logstore.Coverage, key logstore.Pos) (logstore.Pos, bool) {
	newer := w.q.mode == logline.Newer
	far := w.q.far(w.ps)
	reach := key
	first := true
	for _, seg := range segmentsFrom(cov, key, w.q.mode) {
		if far != nil && pastFar(seg, *far, w.q.mode) {
			break
		}
		switch {
		case seg.span != nil && newer:
			reach = seg.span.To
		case seg.span != nil:
			reach = seg.span.From
		case !seg.gap.Live():
			continue
		case first && w.fill.fromKey, w.fill.toEnd && seg.gap.Newest(), w.fill.toFar:
		default:
			return clampTo(reach, key, far, w.q.mode), true
		}
		first = false
	}
	return clampTo(reach, key, far, w.q.mode), false
}

// pastFar is whether seg holds no place short of far, the first place past a page's
// range in the direction mode reads.
func pastFar(seg segment, far logstore.Pos, mode logline.Direction) bool {
	switch {
	case seg.span != nil && mode == logline.Newer:
		return seg.span.From.Compare(far) >= 0
	case seg.span != nil:
		return seg.span.To.Compare(far) <= 0
	case mode == logline.Newer:
		return seg.gap.After != nil && seg.gap.After.Next().Compare(far) >= 0
	}
	return seg.gap.Before != nil && seg.gap.Before.Prev().Compare(far) <= 0
}

// clampTo is reach, a walk's last place from key, held short of far, nil for no
// bound, and never behind key, so a range with no place left reads nothing.
func clampTo(reach, key logstore.Pos, far *logstore.Pos, mode logline.Direction) logstore.Pos {
	if far == nil {
		return reach
	}
	if mode == logline.Newer {
		return logstore.MaxPos(key, logstore.MinPos(reach, far.Prev()))
	}
	return logstore.MinPos(key, logstore.MaxPos(reach, far.Next()))
}

// mark sets the hole each of recs carries, recs in the walk's order. A hole between
// two goes to the later in time. The record at the walk's older edge, a newer walk's
// first or an older walk's last, looks back past the edge for its own, since the line
// before it that the view passes may lie outside what the walk read.
func (w walker) mark(recs []logline.Record, cov logstore.Coverage, held func(logstore.Pos) bool) error {
	if len(recs) == 0 {
		return nil
	}
	edge, before := 0, -1
	if w.q.mode == logline.Older {
		edge, before = len(recs)-1, 1
	}
	for i := range recs {
		if i != edge {
			recs[i].Gap = holesBetween(cov.Gaps, w.ps.committed, lineOf(recs[i+before]).Pos, lineOf(recs[i]).Pos)
		}
	}
	l := looker{r: w.r, row: w.ps.row.ID, cov: cov, committed: w.ps.committed, held: held, keep: w.q.keep, bytes: w.look}
	var err error
	recs[edge].Gap, err = l.back(lineOf(recs[edge]).Pos)
	return err
}

// record is l as a line of the page.
func (w walker) record(l logstore.Line) logline.Record {
	return recordOf(l, w.ps.key, w.ps.place, w.ps.row.ContainerID)
}

// recordOf is l, a line of the instance containerID of key's stream running in place,
// as a page or a tail answers it.
func recordOf(l logstore.Line, key logline.StreamKey, place logline.Place, containerID string) logline.Record {
	return logline.Record{
		Timestamp: time.Unix(0, l.TS).UTC(), Message: l.Message, Stream: key, Place: place,
		ContainerID: containerID, Ordinal: l.Ord,
	}
}

// heldBy is whether a chunk of chunks, oldest first as Chunks reads them, claims a
// line at a position: a line between two chunks of a span is another pattern's,
// which the page's chunks do not vouch for.
func heldBy(chunks []logstore.Chunk) func(logstore.Pos) bool {
	var runs []logstore.Span
	for _, c := range chunks {
		if n := len(runs); n > 0 && c.From.Compare(runs[n-1].To) <= 0 {
			runs[n-1].To = logstore.MaxPos(runs[n-1].To, c.To)
			continue
		}
		runs = append(runs, logstore.Span{From: c.From, To: c.To})
	}
	return func(p logstore.Pos) bool {
		i, found := slices.BinarySearchFunc(runs, p, func(r logstore.Span, p logstore.Pos) int { return r.From.Compare(p) })
		if found {
			return true
		}
		return i > 0 && p.Compare(runs[i-1].To) <= 0
	}
}

// newestOf is the newest line chunks claim, nil with none.
func newestOf(chunks []logstore.Chunk) *logstore.Pos {
	if len(chunks) == 0 {
		return nil
	}
	newest := slices.MaxFunc(chunks, func(a, b logstore.Chunk) int { return a.To.Compare(b.To) }).To
	return &newest
}

// readingOf is a reading of key that has already read recs and ended as it says.
func readingOf(key logline.StreamKey, recs []logline.Record, short bool, stop logline.Cursor, err error) *kubelet.Read {
	out, lines := spsc.New[kubelet.Item](len(recs))
	for _, rec := range recs {
		_ = out.Send(kubelet.Item{Record: rec}) // the buffer holds every line, so nothing blocks or fails
	}
	out.Close()
	if !short {
		stop = logline.Cursor{}
	}
	return &kubelet.Read{Key: key, Lines: lines, Cancel: func() {}, Err: err, Short: short, Stop: stop}
}

// ===== The merge =====

// merge is one direction's page of a scope: its records, oldest first, the cut,
// zero when the page was not cut, each stream it could not read, one line each,
// and how far each stream's walk read.
type merge struct {
	records []logline.Record
	cut     logline.Cursor
	skipped []string
	reaches []streamReach
}

// mergePage is one page of readings, each one container's lines in the order the
// page reads them — oldest first newer, newest first older — merged in
// line order and cut at limit. The cut is where the next page goes on from:
// the last line kept, or the stop of a reading that ran short when that is
// nearer. Every reading is cancelled once the merge has its lines, so no stream
// reads past the page.
func mergePage(ctx context.Context, readings []*kubelet.Read, mode logline.Direction, limit int) (merge, error) {
	recs := merged(ctx, readings, mode, limit+1)
	for _, r := range readings {
		r.Cancel()
		drainReading(r)
	}
	// A reading the context ended closes as one that reached its end does.
	if err := ctx.Err(); err != nil {
		return merge{}, err
	}
	if mode == logline.Older {
		slices.Reverse(recs)
	}
	skipped, err := failures(readings)
	if err != nil {
		return merge{}, err
	}
	for _, r := range readings {
		switch {
		case !r.Short || !r.Stop.IsZero():
		case errors.Is(r.Err, kubelet.ErrRetired):
			return merge{}, errReplaced
		default:
			// Every cut has an edge: a stop with none is a fault.
			return merge{}, fmt.Errorf("logs: %s/%s stopped short before a line in range", r.Key.Pod, r.Key.Container)
		}
	}
	// A page past the limit is cut at the far end, and the cursor is the last line kept.
	var cut logline.Cursor
	if len(recs) > limit {
		if mode == logline.Newer {
			recs = recs[:limit]
			cut = logline.CursorOf(recs[limit-1])
		} else {
			recs = recs[len(recs)-limit:]
			cut = logline.CursorOf(recs[0])
		}
	}
	recs, cut = trimToStops(readings, mode, recs, cut)
	return merge{records: recs, cut: cut, skipped: skipped}, nil
}

// errReplaced is a page a stream of which the connection's retirement cut before
// it read a line in range, so no cursor could go on from it.
var errReplaced = refusal.New(refusal.CodeReplaced, "the connection to the cluster was replaced while this page was read; read it again")

// failures names each reading that ended in an error, one line each, and fails
// the page when the cluster refused the user's credentials, when Kubetail no longer
// connects to it, or when every reading failed. A reading the page's own stop
// ended is no failure, and neither is one that ran short — the connection's
// retirement cut it, or it failed past a line in range — since the page ends where
// it stopped and the next page reads it again.
func failures(readings []*kubelet.Read) ([]string, error) {
	skipped := []string{}
	for _, r := range readings {
		if err := kubelet.FatalError(r.Err); err != nil {
			return nil, err
		}
		if r.Err == nil || r.Short || errors.Is(r.Err, context.Canceled) {
			continue
		}
		skipped = append(skipped, skippedLine(r.Key, r.Err))
	}
	if len(skipped) == len(readings) {
		if err := everyStreamFailed(skipped); err != nil {
			return nil, err
		}
	}
	return skipped, nil
}

// everyStreamFailed refuses a page every stream of which failed, naming the first,
// nil when there was none.
func everyStreamFailed(skipped []string) error {
	if len(skipped) == 0 {
		return nil
	}
	return refusal.New(refusal.CodeReadFailed, "no container of this view could be read: "+skipped[0])
}

// skippedLine is the page's line naming key, a stream err kept it from reading. A
// transport error can carry a URL's userinfo or the server's bytes.
func skippedLine(key logline.StreamKey, err error) string {
	return key.Pod + "/" + key.Container + ": " + logline.StripControl(safe.Safe(err))
}

// beyond is whether a lies past b in the direction mode reads: later newer,
// earlier older.
func beyond(mode logline.Direction, a, b logline.Cursor) bool {
	c := logline.CompareCursors(a, b)
	if mode == logline.Newer {
		return c > 0
	}
	return c < 0
}

// nearerStop is the nearer of stop and r's, the nearest newer being the oldest
// and older the newest, or stop when r did not run short.
func nearerStop(mode logline.Direction, stop logline.Cursor, r *kubelet.Read) logline.Cursor {
	if r.Short && (stop.IsZero() || beyond(mode, stop, r.Stop)) {
		return r.Stop
	}
	return stop
}

// trimToStops keeps the lines on the near side of the nearest point a reading
// that ran short stopped at, and answers that point as the cut, since past it
// some stream's lines were never read. A cut the limit made nearer than it stands.
// recs are oldest first.
func trimToStops(readings []*kubelet.Read, mode logline.Direction, recs []logline.Record, cut logline.Cursor) ([]logline.Record, logline.Cursor) {
	var stop logline.Cursor
	for _, r := range readings {
		stop = nearerStop(mode, stop, r)
	}
	if stop.IsZero() {
		return recs, cut
	}
	i, found := slices.BinarySearchFunc(recs, stop, func(rec logline.Record, p logline.Cursor) int {
		return logline.CompareCursors(logline.CursorOf(rec), p)
	})
	if mode == logline.Newer {
		if found {
			i++
		}
		recs = recs[:i]
	} else {
		recs = recs[i:]
	}
	if cut.IsZero() || beyond(mode, cut, stop) {
		cut = stop
	}
	return recs, cut
}

// merged is the first n lines of readings in the order mode reads them: each
// reading's next line is held, and the nearest goes out. A reading is asked for
// a line only when the merge needs one, so a full page reads nothing past its
// last line. The merge also ends at the first line past a stop, since the page
// is cut there, and at once when a reading runs short before any line in range
// or ctx ends, since the page is refused.
func merged(ctx context.Context, readings []*kubelet.Read, mode logline.Direction, n int) []logline.Record {
	h := &heads{mode: mode}
	var stop logline.Cursor
	// waiting is the readings with no line held.
	waiting := readings
	var out []logline.Record
	for {
		for _, r := range waiting {
			rec, ok := next(ctx, r)
			switch {
			case ok:
				heap.Push(h, headed{rec, r})
			case ctx.Err() != nil, r.Short && r.Stop.IsZero():
				return out
			default:
				stop = nearerStop(mode, stop, r)
			}
		}
		if h.Len() == 0 {
			return out
		}
		top := heap.Pop(h).(headed)
		if !stop.IsZero() && beyond(mode, logline.CursorOf(top.rec), stop) {
			return out
		}
		out = append(out, top.rec)
		if len(out) == n {
			return out
		}
		waiting = []*kubelet.Read{top.from}
	}
}

// next is r's next line, false once r has ended or ctx has.
func next(ctx context.Context, r *kubelet.Read) (logline.Record, bool) {
	select {
	case rec, ok := <-r.Lines.Chan():
		return rec.Record, ok
	case <-ctx.Done():
		return logline.Record{}, false
	}
}

// headed is a reading's next line.
type headed struct {
	rec  logline.Record
	from *kubelet.Read
}

// heads is the lines the readings hold, a heap with the nearest in the order mode
// reads them on top.
type heads struct {
	mode logline.Direction
	h    []headed
}

func (s *heads) Len() int { return len(s.h) }
func (s *heads) Less(i, j int) bool {
	return beyond(s.mode, logline.CursorOf(s.h[j].rec), logline.CursorOf(s.h[i].rec))
}
func (s *heads) Swap(i, j int) { s.h[i], s.h[j] = s.h[j], s.h[i] }
func (s *heads) Push(x any)    { s.h = append(s.h, x.(headed)) }
func (s *heads) Pop() any {
	last := s.h[len(s.h)-1]
	s.h = s.h[:len(s.h)-1]
	return last
}

// drainReading waits for r to end, so how it ended is set.
func drainReading(r *kubelet.Read) {
	for range r.Lines.Chan() {
	}
}

// ===== The caps =====

// capPage applies q's caps to m's records from the edge c reads from: a newer page
// keeps its oldest lines, an older one its newest. A record the page cap dropped
// cuts the page at the last line kept, so no edge passes it. It answers the caps'
// notices.
func (c pageCursor) capPage(m *merge, q FetchRequest) []Notice {
	kept, cut, notices := capLines(m.records, !c.newer, q.MaxMessageBytes, q.MaxPageBytes, q.ForModel)
	m.records = kept
	switch {
	case cut && c.newer:
		m.cut = logline.CursorOf(kept[len(kept)-1])
	case cut:
		m.cut = logline.CursorOf(kept[0])
	}
	return notices
}

// capLines is records, oldest first, under its caps, read from the oldest or, with
// newestFirst, the newest: each message cut to maxLine bytes, redacted first with
// forModel, then the records kept while their messages sum to maxPage. The first
// record read is always kept, with a notice when it alone is over. cut is whether
// a record was dropped, which leaves a line past the page.
func capLines(records []logline.Record, newestFirst bool, maxLine, maxPage int, forModel bool) (kept []logline.Record, cut bool, notices []Notice) {
	out := make([]logline.Record, len(records))
	n, total := 0, 0
	for i := range records {
		j := i
		if newestFirst {
			j = len(records) - 1 - i
		}
		rec := capMessage(records[j], maxLine, forModel)
		total += len(rec.Message)
		if total > maxPage {
			if n > 0 {
				cut = true
				break
			}
			notices = append(notices, Notice{refusal.CodePageByteLimitExceeded, fmt.Sprintf("the page's first line alone is over its %d bytes", maxPage)})
		}
		out[j] = rec
		n++
	}
	if newestFirst {
		return out[len(out)-n:], cut, notices
	}
	return out[:n], cut, notices
}

// capMessage is rec with its message cut to at most max bytes at a character
// boundary, and TruncatedBytes the bytes cut. With forModel the message goes
// through logline.ForModel first: a cut could leave a fragment of a secret the
// redaction no longer knows. A stored line's only control is a kept SGR
// sequence, which is never split: one that does not fit whole is cut off with
// what follows it.
func capMessage(rec logline.Record, max int, forModel bool) logline.Record {
	if forModel {
		rec.Message = logline.ForModel(rec.Message)
	}
	msg := rec.Message
	if len(msg) <= max {
		return rec
	}
	end := 0
	for end < len(msg) {
		_, size := utf8.DecodeRuneInString(msg[end:])
		next := end + size
		if msg[end] == 0x1b {
			next = logline.SkipCSI(msg, end+2)
		}
		if next > max {
			break
		}
		end = next
	}
	rec.Message, rec.TruncatedBytes = msg[:end], len(msg)-end
	return rec
}

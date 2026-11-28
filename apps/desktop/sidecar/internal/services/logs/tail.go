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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amorey/gobus/conflate"
	"github.com/amorey/gochan/mpsc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// ===== The tail =====

// errStopped ends a tail with no stream left running.
var errStopped = refusal.New(refusal.CodeFinished, "every container of this view has stopped")

// tailRetry is how long a tail waits to resolve its view again after the
// mirror refused it, to open its mirror watches again after a clean end, or to
// reopen a stream the connection dropped the first time. Each drop in a row doubles
// the stream's wait, up to tailRetryCap.
const (
	tailRetry    = 2 * time.Second
	tailRetryCap = time.Minute
)

// tailSettle is how long a tail waits after a pod or workload frame before it
// resolves its view again, so a burst of frames costs one resolve.
const tailSettle = 250 * time.Millisecond

// TailRequest is one tail. It starts from now, or from Since, that moment's
// lines included, or from Cursor, a page's Next or Follow: each stream reads past
// where the page left it, and the scope is the cursor's. Seen, sent with either,
// is the last line the caller has of each stream: that stream reads past its
// line, and a stream not in Seen reads from the start. MaxMessageBytes cuts each
// message, 0 for the service's own cap; ForModel redacts it first, as a page's.
type TailRequest struct {
	Scope
	Since           *time.Time
	Seen            []string
	Cursor          *string
	MaxMessageBytes int
	ForModel        bool
}

// TailResponse is a tail as it opened: Request as it ran, the scope decoded from
// the cursor on a token start; ResolvedAt, the first page's clock the cursor carries,
// or now; and Frames, the tail itself.
type TailResponse struct {
	Request    TailRequest
	ResolvedAt time.Time
	Frames     *cluster.Stream[TailFrame]
}

// TailFrame is one frame of a tail, exactly one field set. Record is a line.
// Notice is something the tail wants the caller to know. Ended is why the
// tail ended, and is the last frame: tailing again would end the same way.
type TailFrame struct {
	Record *logline.Record
	Notice *Notice
	Ended  *refusal.Refusal
}

// tailBatch and tailBatchBytes are the most lines the tail's walks read, and
// the most bytes of lines they keep, across every stream, before it sends them and
// walks on.
const (
	tailBatch      = 1000
	tailBatchBytes = 4 << 20
)

// TailFromCursor is q's scope's lines on cluster id as they are written, from q's start.
// The lines are read from the cluster cache's log file, which a tailer per stream
// writes, shared by every tail of it: each stream is walked from its own
// watermark on every commit to it, and its lines go out in line order, so lines
// are ordered per container. A scope with no container yet — a Job not
// scheduled, a Deployment at zero — waits for its pods.
//
//  1. A tail starts from now, from Since, a moment whose lines are in, or from
//     Cursor, a page's Next or Follow, whose scope it reads. Cursor beside Since,
//     or Seen beside neither, is conflicting-parameters.
//  2. Seen is the last line the caller has of each stream: that stream reads
//     past it, never before the start; every other from the start. Beside a
//     Cursor it merges into the cursor's entries, the newer line standing.
//  3. A tail from a Cursor reads every line a poll of it would, then goes on
//     past the window's end: it ignores the cursor's Until and After.
//  4. The grep applies to every line sent, padding included, and each message
//     is cut at MaxMessageBytes, saying the bytes cut in TruncatedBytes; with
//     ForModel, cut after logline.ForModel.
//  5. A stream the server refuses, ends or finds too loud sends a stream-skipped
//     Notice once, unless the server ends it because its container is waiting to
//     start.
//  6. A tail hands out no cursor: what a caller needs to go on is its start
//     and the lines it saw, sent back as Seen.
//  7. A tail whose start is not now first reads the instances only the file
//     holds, its Pod sources' and its workloads' pods the mirror no longer holds,
//     from its start to their ends, then tails. A scope of gone pods alone under
//     a label filter the mirror has no nodes for then ends no-nodes.
//
// A tail that ends for a reason the user can read — the claim revoked, the
// cluster refusing the user's credentials, every container of the scope stopped,
// every source of it a Pod whose pod was deleted, a label filter with no nodes
// to apply it over, the scope grown past what
// Kubetail reads at once, a kind the cache no longer mirrors — sends one Ended frame
// saying so and ends clean, since tailing again would end the same way. Any
// other ending sets Err, the log file closing under it included, and its context
// ending is a clean end with no frame.
func (s *Service) TailFromCursor(ctx context.Context, id cluster.ClusterID, q TailRequest) (TailResponse, error) {
	maxMessage, err := messageCap(q.MaxMessageBytes)
	if err != nil {
		return TailResponse{}, err
	}
	start, err := tailStartCursor(q, s.now())
	if err != nil {
		return TailResponse{}, err
	}
	scope := start.Scope
	grep, err := logline.CompileGrep(scope.Grep)
	if err != nil {
		return TailResponse{}, err
	}
	t, err := resolve(ctx, s.clusters, id, "", scope, true)
	if err != nil {
		return openFailed(ctx, q, err)
	}
	// owned ends the mirror watches and every tailer's watch on any ending. The
	// stream itself runs on ctx: one ended by its own context files no error.
	owned, stop := context.WithCancel(ctx)
	f := &cursorTail{
		s: s, id: id, scope: scope, start: start, maxMessage: maxMessage, forModel: q.ForModel, grep: grep, tails: s.registryFor(id, t), target: t,
		streams: map[logline.StreamKey]*tracked{}, nodes: map[string]logline.Place{},
		deleted: map[string]string{}, podUIDs: map[string]string{}, gone: map[string]bool{}, held: map[int64]func(){},
		dueHub: mpsc.New[logline.StreamKey](0, mpsc.WithKeepOpen()), endedHub: mpsc.New[*handle](0, mpsc.WithKeepOpen()),
		filled: make(chan fillDone),
	}
	if err := f.open(owned, q.Since == nil && q.Cursor == nil); err != nil {
		stop()
		f.close()
		return openFailed(ctx, q, err)
	}
	q.Scope, q.MaxMessageBytes = scope, maxMessage
	frames := cluster.NewStream(ctx, func(ctx context.Context, out chan<- TailFrame) error {
		defer f.close()
		// A fill in flight reads over the claim and writes the file: both outlive it.
		defer f.fills.Wait()
		defer stop()
		f.out = out
		err := f.run(owned)
		// A cancel ends the watches too, which can read as an ending of their own.
		if ctx.Err() != nil {
			return nil
		}
		if r, ok := refusal.As(err); ok {
			send(ctx, out, TailFrame{Ended: &r})
			return nil
		}
		return err
	})
	return TailResponse{Request: q, ResolvedAt: start.ResolvedAt, Frames: frames}, nil
}

// openFailed answers a tail that failed to open with err. One whose context
// ended as it opened ends clean, as it would once open: no error, and frames that
// end at once.
func openFailed(ctx context.Context, q TailRequest, err error) (TailResponse, error) {
	if ctx.Err() == nil {
		return TailResponse{}, err
	}
	frames := cluster.NewStream(ctx, func(context.Context, chan<- TailFrame) error { return nil })
	return TailResponse{Request: q, Frames: frames}, nil
}

// tailStartCursor is q's start cursor, which holds its scope and seeds every stream:
// from a moment, a first page from Since, now with none, with q's Seen as its
// entries; from a page cursor, the cursor with q's Seen merged into its entries.
func tailStartCursor(q TailRequest, now time.Time) (pageCursor, error) {
	switch {
	case q.Cursor != nil && q.Since != nil:
		return pageCursor{}, refusal.New(refusal.CodeConflictingParameters, "a tail starts from since or from a page cursor, not both")
	case q.Cursor != nil && !q.Scope.empty():
		return pageCursor{}, refusal.New(refusal.CodeConflictingParameters, "a page cursor holds its scope, so a tail from one sends none")
	case q.Cursor != nil:
		return cursorStart(*q.Cursor, q.Seen)
	case len(q.Sources) == 0:
		return pageCursor{}, refusal.New(refusal.CodeConflictingParameters, "a tail sends a scope or a page cursor")
	case q.Since == nil && len(q.Seen) > 0:
		return pageCursor{}, refusal.New(refusal.CodeConflictingParameters, "seen goes with since or a page cursor: a stream it does not name would read from now")
	}
	since := now
	if q.Since != nil {
		since = *q.Since
	}
	return firstPage(FetchRequest{Scope: q.Scope, Window: Window{Since: &since}, PageFrom: PageFromFirst, Seen: q.Seen}, now)
}

// cursorStart is the start cursor a tail from the page cursor s reads from, seen
// merged into its entries. A tail does not stop, so the window's end goes: its
// Until, its After and the count past the range.
func cursorStart(s string, seen []string) (pageCursor, error) {
	c, err := decodePageCursor(s)
	if err != nil {
		return pageCursor{}, err
	}
	if !c.newer {
		return pageCursor{}, refusal.New(refusal.CodeCursorInvalid, "a tail goes on from a page's next or follow, not its prev")
	}
	if c.seen, err = mergeSeen(c.seen, seen); err != nil {
		return pageCursor{}, err
	}
	c.Until, c.Window.After, c.after = nil, Padding{}, 0
	return c, nil
}

// open takes what the tail reads over: a claim on the cluster, the cache's log
// file, the rows of the instances only the file holds, which it reads first, and
// its mirror watches. A tail from now reads no such instance, since nothing of
// one is stamped after now. A label filter the mirror has no nodes for is refused
// here only with no such instance to read; with one, run reads it first.
func (f *cursorTail) open(ctx context.Context, fromNow bool) error {
	t := f.target
	var err error
	if f.claim, err = kubelet.Open(ctx, f.s.clusters, f.id, t.serverUID, f.s.retry); err != nil {
		return err
	}
	for _, b := range f.s.backends {
		f.backends, f.names = append(f.backends, b.build(f.claim.Get)), append(f.names, b.name)
	}
	if f.file, err = f.s.files.OpenOrCreate(int64(t.cacheID)); err != nil {
		return err
	}
	if !fromNow {
		start := pageQuery{scope: f.scope, mode: logline.Newer, cur: &f.start}
		if f.only, f.releaseOnly, err = f.s.fileOnlyStreams(ctx, f.file, t, []pageQuery{start}); err != nil {
			return err
		}
	}
	if t.noNodes != nil && len(f.only) == 0 {
		return t.noNodes
	}
	f.notePods(t)
	return f.watch(ctx, t.cacheID)
}

// close lets go of what open took, as far as it got, and every row the tail holds.
func (f *cursorTail) close() {
	for _, release := range f.held {
		release()
	}
	if f.releaseOnly != nil {
		f.releaseOnly()
	}
	if f.file != nil {
		f.file.Close()
	}
	if f.claim != nil {
		f.claim.Close()
	}
}

// keep is the tail's test of a line: the grep, every line, padding included.
func (f *cursorTail) keep(_ int64, message string) bool { return logline.Matches(f.grep, message) }

// objectWatch is a watch of mirrored objects.
type objectWatch = *cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame]

// watches is what a tail watches of the mirror besides its streams, and the
// cancel that ends them all. workloads carries the watches of the view's workload
// kinds, each a frame at a time.
type watches struct {
	pods, nodes objectWatch
	workloads   *mpsc.Hub[workloadEvent]
	stop        context.CancelFunc
}

// workloadEvent is a frame of the workload watch w past its snapshot, the bookmark
// closing it included, or w's end.
type workloadEvent struct {
	w     objectWatch
	ended bool
}

// watch opens the tail's mirror watches on cacheID. A node's labels moving sends
// no pod frame, so the nodes are watched too, and so is each workload kind the
// view walks through to its pods, since an object of one arriving late or changing
// owner sends none either. Only the view's namespaces' objects can move it, and
// nothing reads a pod's or a workload's body.
func (f *cursorTail) watch(ctx context.Context, cacheID cluster.ClusterCacheID) error {
	ctx, stop := context.WithCancel(ctx)
	data := f.s.clusters.CachedData()
	namespaces := f.scope.namespaces()
	w := &watches{workloads: mpsc.New[workloadEvent](0, mpsc.WithKeepOpen()), stop: stop}
	var err error
	w.pods, err = data.WatchObjectCollectionByNamespaces(ctx, f.id, cacheID, "v1", "pods", namespaces)
	if err == nil {
		w.nodes, err = data.WatchObjectCollection(ctx, f.id, cacheID, "v1", "nodes")
	}
	for _, kind := range f.scope.workloadKinds() {
		if err != nil {
			break
		}
		var kw objectWatch
		kw, err = data.WatchObjectCollectionByNamespaces(ctx, f.id, cacheID, kind.apiVersion, kind.resource, namespaces)
		if err == nil {
			w.forward(ctx, kw)
		}
	}
	if err != nil {
		stop()
		return err
	}
	f.w = w
	return nil
}

// forward sends kw's frames past its snapshot on workloads, then its end. It stops
// once ctx ends, which ends kw too.
func (w *watches) forward(ctx context.Context, kw objectWatch) {
	events := w.workloads.Sender()
	go func() {
		defer events.Close()
		synced := false
		for frame := range kw.Frames {
			synced = synced || frame.Type == cluster.CollectionWatchFrameBookmark
			if synced && events.SendContext(ctx, workloadEvent{w: kw}) != nil { // coverage:ignore only the tail ending while a frame is in flight
				return
			}
		}
		_ = events.SendContext(ctx, workloadEvent{w: kw, ended: true}) // an error is the watches stopping, which no longer read it
	}()
}

// watchEnded takes the end of a mirror watch: its error, or, for a clean end — the
// cache cleared or torn down — every watch closed, to be opened again by the
// reconcile after the retry, whose resolve says what became of the cache.
func (f *cursorTail) watchEnded(w objectWatch) error {
	if err := w.Err(); err != nil {
		return err
	}
	f.w.stop()
	f.w, f.synced, f.nodesSynced, f.nodes = nil, false, false, map[string]logline.Place{}
	f.askAgain()
	return nil
}

// cursorTail is one tail's state. Its streams are reconciled against the view after
// every event — a pod or workload frame, a node's labels moving, a tailer's end, a
// retry — by one rule: a container the view wants is tailed unless it ended at the
// generation the view sees now.
type cursorTail struct {
	s     *Service
	id    cluster.ClusterID
	scope Scope
	grep  *regexp.Regexp
	claim *kubelet.Claim
	// start is the tail's start cursor: every stream, at open or joining later,
	// starts at its keyFor. maxMessage is where each message is cut, after
	// logline.ForModel with forModel.
	start      pageCursor
	maxMessage int
	forModel   bool
	// file is the tail's claim on the cache's log file, tails its tailers, and
	// backends and names the cluster's backends over the tail's claim, which fill a
	// live gap a walk meets.
	file     *logstore.Store
	tails    *registry
	backends []kubelet.Backend
	names    []string
	target   target
	// only is the instances of the scope's Pod sources only the file holds, read
	// once before anything is tailed, and releaseOnly lets go of their rows.
	only        []pageStream
	releaseOnly func()
	out         chan<- TailFrame
	// w is the tail's mirror watches, nil while they are to be opened again.
	w *watches
	// streams is what the tail knows of each stream it has wanted, and wanted the
	// streams the view wants now, in its order.
	streams map[logline.StreamKey]*tracked
	wanted  []logline.StreamKey
	// pings is the file's bus under pingRows, the rows of the streams the view wants.
	pings    *conflate.Receiver[int64, struct{}]
	pingRows []int64
	// held is the release of each row the tail has filed, which it holds until it
	// ends so no sweep takes a row it may fill.
	held map[int64]func()
	// batch is the lines walked and not yet sent.
	batch []logline.Record
	// lines and bytes are what the walks may still read and keep before the next send.
	lines, bytes int
	// again fires when a resolve the mirror refused is due to be asked again, or
	// the mirror watches opened again; settled fires once the frames a resolve
	// waits on have settled. rejudge is set when a stream ended at its generation,
	// so the view's end is judged again.
	again, settled <-chan time.Time
	rejudge        bool
	// dueHub carries each stream that dropped and has waited out its backoff, from a
	// timer of its own, and endedHub each tailer that ended.
	dueHub   *mpsc.Hub[logline.StreamKey]
	endedHub *mpsc.Hub[*handle]
	// filled carries each gap fill's end from its goroutine, and fills counts the
	// fills in flight.
	filled chan fillDone
	fills  sync.WaitGroup
	// synced is whether the pods watch's snapshot has closed, and nodesSynced the
	// nodes watch's.
	synced, nodesSynced bool
	// nodes is each node's place a line carries, as the nodes watch last sent it.
	nodes map[string]logline.Place
	// deleted is each Pod source's pod the pods watch deleted, by "<ns>/<name>",
	// with its UID, for the next resolve to judge: gone unless the mirror holds that
	// pod again, as a cache's clear and relist leave it.
	deleted map[string]string
	// podUIDs is the UID of each Pod source's pod as a resolve first read it, by
	// "<ns>/<name>", so a pod of its name with another UID, or none in a snapshot,
	// reads as gone even when no Deleted frame came, as across a cache's clear.
	podUIDs map[string]string
	// snapshot is whether a pods watch's snapshot has closed since a resolve last
	// judged the pods: only then is a pod the mirror lacks known to be gone, not
	// yet to be listed again.
	snapshot bool
	// gone is each Pod source's pod that is gone, by "<ns>/<name>". Its source
	// leaves the view, and a pod of the same name later is another pod, which only
	// another source can read.
	gone map[string]bool
}

// tracked is one stream as the tail knows it: the stream as the view last wanted
// it, its file row, the tailer it holds, nil while it holds none, since when, and
// unwatch, which stops telling the tail of that tailer's end, and its watermark,
// past every line it sent of the row. A stream that ended on its own has no tailer,
// and records the generation it ended at, with the cluster's refusal when that is how
// it ended. A stream whose tailer dropped is held for its backoff, then the next
// reconcile attaches it again. drops is how many of its tailers dropped in a row.
// restarted is set once the view sees a later instance of the container than the
// one the tailer reads, whose end is then its log's, however the old instance stood.
// filling is set while a fill of its row's live gap is in flight, which no walk of
// it waits on: its watermark stays at the gap until the fill ends. crossed and
// release are what the walk after a fill takes: that the gap at its watermark was
// read, and the hold on what the fill wrote. looked is set once the tail has sent
// a line of the stream, the first looking back for its holes, and gap is the holes
// its walks crossed since the last line it sent, which the next carries. skipped is
// the generation whose failure the tail last sent a notice for, zero for none.
type tracked struct {
	st        stream
	row       int64
	h         *handle
	attached  time.Time
	unwatch   context.CancelFunc
	wm        logstore.Pos
	ended     bool
	endedAt   generation
	refused   string
	held      bool
	drops     int
	restarted bool
	filling   bool
	crossed   bool
	release   func()
	looked    bool
	gap       *logline.Gap
	skipped   generation
}

// tail is where tr's tailer reads on from, every gap past which is the tailer's;
// nil with no tailer.
func (tr *tracked) tail() *logstore.Pos {
	if tr.h == nil {
		return nil
	}
	start := tr.h.Start()
	return &start
}

// committed is how far tr's tailer has written into the file, its start at least;
// nil with no tailer, which leaves no gap tailed.
func (tr *tracked) committed() *logstore.Pos {
	if tr.h == nil {
		return nil
	}
	c := tr.h.Committed()
	return &c
}

// pageStream is tr's stream as a page's fill reads it.
func (tr *tracked) pageStream() pageStream {
	key := tr.st.key
	return pageStream{
		row: logstore.Stream{ID: tr.row, Namespace: key.Namespace, Pod: key.Pod, Container: key.Container, ContainerID: tr.st.gen.instance},
		st:  &tr.st, key: key, place: tr.st.place,
	}
}

// record is l, a line of tr's stream, as the tail sends it.
func (tr *tracked) record(l logstore.Line) logline.Record {
	return recordOf(l, tr.st.key, tr.st.place, tr.st.gen.instance)
}

// run reads the instances only the file holds, brings the target's streams up,
// then follows the view's pods and walks its streams on every commit until an
// ending.
func (f *cursorTail) run(ctx context.Context) error {
	defer func() {
		for _, tr := range f.streams {
			f.detach(tr)
		}
		if f.pings != nil {
			f.pings.Close()
		}
	}()
	if err := f.readOnly(ctx); err != nil {
		return err
	}
	if f.target.noNodes != nil {
		// No resolve judges a pod gone without nodes, so a scope of gone pods alone
		// would wait for nodes forever.
		if f.onlyGonePods() {
			return f.target.noNodes
		}
		f.askAgain()
	}
	if err := f.bring(ctx, f.target); err != nil {
		return err
	}
	return f.relay(ctx)
}

// readOnly sends the lines of the instances only the file holds, from the start
// cursor to their ends, then lets their rows go: one newer read through the page
// engine, a page of MaxPageSize at a time, each going on from the last's edge as
// a Next does. They never grow, so they are read once, and no backend serves them.
func (f *cursorTail) readOnly(ctx context.Context) error {
	if len(f.only) == 0 {
		return nil
	}
	defer func() {
		f.releaseOnly()
		f.releaseOnly = nil
	}()
	cur := f.start
	for {
		q := pageQuery{scope: f.scope, grep: f.grep, mode: logline.Newer, cur: &cur, limit: MaxPageSize, grepAll: true}
		ms, err := f.s.pageOver(ctx, f.id, f.target, f.file, f.only, "", q)
		if err != nil {
			return err
		}
		m := ms[0]
		for _, line := range m.skipped { // coverage:ignore a stream only the file holds is never filled, so never skipped
			if !send(ctx, f.out, TailFrame{Notice: &Notice{refusal.CodeStreamSkipped, line}}) {
				return ctx.Err()
			}
		}
		for _, rec := range m.records {
			if !f.sendLine(ctx, rec) {
				return ctx.Err()
			}
		}
		if m.cut.IsZero() {
			return nil
		}
		next, _ := cur.edges(m)
		// A cut page that moves no stream would be read again forever.
		if maps.Equal(next.seen, cur.seen) { // coverage:ignore a guard: a cut page moves the stream it cut
			return errors.New("tail: a page of the instances only the file holds did not move")
		}
		cur = next
	}
}

// onlyGonePods is whether every source of the scope is a Pod source whose pod the
// mirror no longer holds.
func (f *cursorTail) onlyGonePods() bool {
	for _, src := range f.scope.Sources {
		if _, live := f.target.pods[src.Namespace+"/"+src.Name]; src.Kind != SourcePod || live {
			return false
		}
	}
	return true
}

// relay walks the streams each ping names and reconciles the streams on every event
// until an ending, which it answers.
func (f *cursorTail) relay(ctx context.Context) error {
	for {
		var again bool
		var err error
		// A nil channel never delivers, so no frame is read while the watches are closed.
		var pods, nodes <-chan cluster.ClusterCachedDataObjectCollectionWatchFrame
		var workloads <-chan workloadEvent
		if f.w != nil {
			pods, nodes, workloads = f.w.pods.Frames, f.w.nodes.Frames, f.w.workloads.Receiver().Chan()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-f.claim.Revoked:
			return kubelet.ErrRevoked
		case ping, ok := <-f.pings.Chan():
			if !ok {
				return fmt.Errorf("tail: %w", logstore.ErrClosed)
			}
			err = f.walkRows(ctx, f.pinged(ping.Key))
		case <-f.again:
			again = true
		case <-f.settled:
			again = true
		case h := <-f.endedHub.Receiver().Chan():
			err = f.ended(ctx, h)
		case d := <-f.filled:
			err = f.filledGap(ctx, d)
		case key := <-f.dueHub.Receiver().Chan():
			if t := f.streams[key]; t != nil {
				t.held = false
			}
			again = true
		case frame, ok := <-pods:
			switch {
			case !ok:
				err = f.watchEnded(f.w.pods)
			case f.podsMoved(frame, pods):
				again = f.settle()
			}
		case frame, ok := <-nodes:
			if !ok {
				err = f.watchEnded(f.w.nodes)
			} else {
				again = f.nodesMoved(frame, nodes)
			}
		case e := <-workloads:
			if e.ended {
				err = f.watchEnded(e.w)
			} else {
				again = f.settle()
			}
		}
		if err != nil {
			return err
		}
		if again || f.rejudge {
			if err := f.reconcile(ctx); err != nil {
				return err
			}
		}
	}
}

// pinged is row and every row whose ping is already waiting behind it.
func (f *cursorTail) pinged(row int64) []int64 {
	rows := []int64{row}
	more, _ := f.pings.TryRecvAll() // an error is the bus closed, which the next receive reports
	for _, ping := range more {
		rows = append(rows, ping.Key)
	}
	return rows
}

// askAgain arms the retry, unless it is armed already.
func (f *cursorTail) askAgain() {
	if f.again == nil {
		f.again = time.After(f.s.retry)
	}
}

// settle is whether a pod or workload frame's resolve runs now: with no settle it
// does, and otherwise it waits for the frames after it, the first frame arming the
// wait.
func (f *cursorTail) settle() bool {
	if f.s.settle == 0 {
		return true
	}
	if f.settled == nil {
		f.settled = time.After(f.s.settle)
	}
	return false
}

// sendLine sends rec, its message cut at the tail's cap, and is false once ctx
// has ended.
func (f *cursorTail) sendLine(ctx context.Context, rec logline.Record) bool {
	rec = capMessage(rec, f.maxMessage, f.forModel)
	return send(ctx, f.out, TailFrame{Record: &rec})
}

// send puts frame on out, and is false once ctx has ended.
func send(ctx context.Context, out chan<- TailFrame, frame TailFrame) bool {
	select {
	case out <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}

// track is what the tail knows of key's stream, new when nothing yet.
func (f *cursorTail) track(key logline.StreamKey) *tracked {
	t := f.streams[key]
	if t == nil {
		t = &tracked{}
		f.streams[key] = t
	}
	return t
}

// attach holds st's tailer for tr, its row's, and tells endedHub once it ends.
func (f *cursorTail) attach(ctx context.Context, tr *tracked) error {
	h, err := f.tails.attach(ctx, tr.st, tr.row, tr.wm)
	if err != nil {
		return err
	}
	ctx, unwatch := context.WithCancel(ctx)
	tr.h, tr.attached, tr.unwatch = h, f.s.now(), unwatch
	ended := f.endedHub.Sender()
	go func() {
		defer ended.Close()
		select {
		case <-h.Done():
			_ = ended.SendContext(ctx, h) // an error is the tail ending, which no longer reads it
		case <-ctx.Done():
		}
	}()
	return nil
}

// detach lets go of tr's tailer, if it holds one.
func (f *cursorTail) detach(tr *tracked) {
	if tr.h != nil {
		tr.unwatch()
		tr.h.Release()
		tr.h, tr.unwatch = nil, nil
	}
}

// ended takes the end of the tailer h: an ending of the tail when it was Kubetail
// no longer connecting to the cluster, else the stream walked once more, since the
// tailer's last lines are in the file, and judged by how the tailer ended. A log
// that ended with its instance may have lines a page cached past the tailer's last,
// which the walk under the tailer stops short of: once judged ended, the stream is
// walked to its end as a finished instance is, filling what the file lacks.
func (f *cursorTail) ended(ctx context.Context, h *handle) error {
	var tr *tracked
	for _, t := range f.streams {
		if t.h == h {
			tr = t
		}
	}
	if tr == nil { // coverage:ignore only a tailer's end racing its stream's detach
		return nil
	}
	if f.claim.IsRevoked() { // coverage:ignore only a revocation racing a tailer's end, which relay takes first
		return kubelet.ErrRevoked
	}
	if err := f.walkStreams(ctx, []*tracked{tr}); err != nil {
		return err
	}
	if err := f.failed(ctx, tr, h.Err()); err != nil {
		return err
	}
	if tr.ended && tr.st.finished() {
		return f.walkStreams(ctx, []*tracked{tr})
	}
	return nil
}

// failed takes how tr's stream stopped being read, by its tailer's end or a fill
// that failed, letting its tailer go: an ending of the tail when it was the
// cluster refusing the user's credentials. A stream the connection dropped, or the
// server failed, is held for its backoff, and so is a clean end on a container the
// view sees running, since a proxy or the API server can close an idle stream. Any
// other end is the server's answer for this log: the stream is tailed again once the
// view sees a later generation of it, and the view's end is judged again. One that
// ended on an error says so in a stream-skipped notice, once a generation, unless
// the server only answered that its container is waiting to start.
func (f *cursorTail) failed(ctx context.Context, tr *tracked, err error) error {
	ran := time.Duration(0)
	if tr.h != nil {
		ran = f.s.now().Sub(tr.attached)
	}
	f.detach(tr)
	kind := kubelet.FailureKind(err)
	switch {
	case kind == kubelet.FailFatal:
		return kubelet.FatalError(err)
	case kind == kubelet.FailTransient, kind == kubelet.FailNone && !tr.restarted && !tr.st.key.Previous && tr.st.gen.state == stateRunning:
		f.hold(ctx, tr, ran)
		return nil
	}
	refused := ""
	if kind == kubelet.FailDenied {
		refused = err.Error()
	}
	tr.ended, tr.endedAt, tr.refused = true, tr.st.gen, refused
	f.rejudge = true
	// A waiting container's log is still to come, so its refusal is no skip.
	waiting := kind == kubelet.FailEnded && tr.st.gen.state == stateWaiting
	if kind == kubelet.FailNone || waiting || tr.skipped == tr.st.gen {
		return nil
	}
	tr.skipped = tr.st.gen
	send(ctx, f.out, TailFrame{Notice: &Notice{refusal.CodeStreamSkipped, skippedLine(tr.st.key, err)}}) // false is the tail ending, which the loop sees
	return nil
}

// hold keeps the dropped stream tr, which ran for ran, untailed for its backoff,
// then sends its key on dueHub. A held stream is read again, so it is not ended,
// though its tailer's end judged it so while a fill of it was in flight.
func (f *cursorTail) hold(ctx context.Context, tr *tracked, ran time.Duration) {
	wait := f.backoff(tr, ran)
	tr.held, tr.ended = true, false
	due := f.dueHub.Sender()
	key := tr.st.key // read here: the tail's goroutine owns tr
	time.AfterFunc(wait, func() {
		defer due.Close()
		_ = due.SendContext(ctx, key) // an error is the tail ending, which no longer reads it
	})
}

// backoff counts a drop of t's stream, which ran for ran, and is how long t waits:
// the service's retry, doubled for each drop in a row, up to retryCap. A stream
// that ran for retryCap or longer starts the count again, so an idle stream a proxy
// closes now and then reopens after the retry alone.
func (f *cursorTail) backoff(t *tracked, ran time.Duration) time.Duration {
	if ran >= f.s.retryCap {
		t.drops = 0
	}
	wait := min(f.s.retry<<min(t.drops, 16), f.s.retryCap)
	t.drops++
	return wait
}

// A running container's state in the mirror, and one waiting to start.
const (
	stateRunning = "running"
	stateWaiting = "waiting"
)

// podsMoved takes frame and every frame already waiting behind it, and is whether
// the view is to be resolved again: when the watch's opening snapshot closed among
// them, since the view was resolved before the watch opened, or when one is a pod
// past it, every one of which is in the view's namespaces. A Pod source's pod
// deleted is kept for that resolve to judge.
func (f *cursorTail) podsMoved(frame cluster.ClusterCachedDataObjectCollectionWatchFrame, more <-chan cluster.ClusterCachedDataObjectCollectionWatchFrame) bool {
	relevant := false
	for {
		switch {
		case frame.Type == cluster.CollectionWatchFrameBookmark:
			f.synced, f.snapshot, relevant = true, true, true
		case f.synced:
			f.noteDeleted(frame)
			relevant = relevant || frame.Object != nil
		}
		select {
		case next, ok := <-more:
			if !ok { // coverage:ignore only the pods watch closing while frames wait behind one
				return relevant
			}
			frame = next
		default:
			return relevant
		}
	}
}

// nodesMoved takes frame and every frame already waiting behind it, and is whether
// the view is to be resolved again: when the watch's opening snapshot closed among
// them, since the view was resolved before the watch opened, or when a node past
// it came, went, or moved one of the labels a line carries. A node's other changes
// are its status, which a tail does not read. With no filter on a label, only a
// node a stream runs on can move the view, and no other is read.
func (f *cursorTail) nodesMoved(frame cluster.ClusterCachedDataObjectCollectionWatchFrame, more <-chan cluster.ClusterCachedDataObjectCollectionWatchFrame) bool {
	relevant := false
	labels := filtersLabels(f.scope.Filters)
	for {
		switch {
		case frame.Type == cluster.CollectionWatchFrameBookmark:
			f.nodesSynced, relevant = true, true
		case frame.Object == nil:
		case !labels && !f.runsOn(frame.Object.Name):
			delete(f.nodes, frame.Object.Name)
		case frame.Type == cluster.CollectionWatchFrameDeleted:
			delete(f.nodes, frame.Object.Name)
			relevant = relevant || f.nodesSynced
		default:
			place, ok := placeOf(frame.Object.Name, string(frame.Object.RawJSON))
			was, known := f.nodes[frame.Object.Name]
			f.nodes[frame.Object.Name] = place
			relevant = relevant || f.nodesSynced && (!ok || !known || was != place)
		}
		select {
		case next, ok := <-more:
			if !ok {
				return relevant
			}
			frame = next
		default:
			return relevant
		}
	}
}

// filtersLabels is whether any of filters reads a node's labels.
func filtersLabels(filters []ScopeFilter) bool {
	for _, flt := range filters {
		if flt.Field != FilterNode {
			return true
		}
	}
	return false
}

// runsOn is whether a stream the tail reads runs on node.
func (f *cursorTail) runsOn(node string) bool {
	for _, t := range f.streams {
		if t.h != nil && t.st.place.Node == node {
			return true
		}
	}
	return false
}

// placeOf is the place of node name, whose body is raw, as resolve reads it, and
// false for a body that does not parse.
func placeOf(name, raw string) (logline.Place, bool) {
	var node struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return logline.Place{}, false
	}
	l := node.Metadata.Labels
	return logline.Place{Node: name, Region: l[labelRegion], Zone: l[labelZone], OS: l[labelOS], Arch: l[labelArch]}, true
}

// reconcile resolves the view and brings the streams to it. A mirror with no cache,
// or still syncing, or holding no node a label filter reads, is asked again after
// the service's retry, since its recovery sends no pod frame. Closed mirror watches
// open again on the cache the view resolved against.
func (f *cursorTail) reconcile(ctx context.Context) error {
	f.again, f.settled, f.rejudge = nil, nil, false
	t, ok, err := f.resolve(ctx)
	if !ok || err != nil {
		return err
	}
	if f.w == nil {
		if err := f.watch(ctx, t.cacheID); err != nil {
			return err
		}
	}
	return f.bring(ctx, t)
}

// bring brings the streams to t: each is filed in the file; a stream that left the
// view lets go of its tailer, and so does one whose pod another of its name on
// another node replaced, while a restarted container's tailer runs on until its log
// ends. A stream the view wants is tailed unless it is tailed already, held for its
// backoff, or ended at the generation the view sees; one that joins starts at the
// tail's start. An instance that has finished gets no tailer: its log never grows,
// so it is walked to its end once. Then each stream just tailed is walked, and every
// one when the bus's receiver was made anew for the streams the view wants. A view whose every stream has ended, none of them
// waiting to start or held and no container of it starting, ends the tail; one
// with no stream waits for one.
func (f *cursorTail) bring(ctx context.Context, t target) error {
	rows, err := fileStreams(ctx, f.file, t.streams)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if f.held[row.ID] == nil {
			f.held[row.ID] = f.file.Hold(row.ID)
		}
	}
	want := map[logline.StreamKey]int{}
	for i, st := range t.streams {
		want[st.key] = i
	}
	for key, tr := range f.streams {
		if tr.h == nil {
			continue
		}
		i, ok := want[key]
		switch {
		case !ok:
			f.detach(tr)
		case rows[i].ID == tr.row:
			// The place a line carries follows the node's labels.
			tr.st = t.streams[i]
		case t.streams[i].place != tr.st.place:
			// A pod of its name elsewhere replaced its pod, whose log is gone.
			f.detach(tr)
		default:
			// A restarted container's old tailer runs on until its log ends.
			tr.restarted = true
		}
	}
	ids := rowIDs(rows)
	// A restarted container's old tailer writes its own row until its log ends.
	for _, tr := range f.streams {
		if tr.h != nil && !slices.Contains(ids, tr.row) {
			ids = append(ids, tr.row)
		}
	}
	slices.Sort(ids)
	resubscribed, err := f.subscribe(ids)
	if err != nil { // coverage:ignore only the file closing between filing the streams and subscribing
		return err
	}
	f.wanted = f.wanted[:0]
	var drain []*tracked
	var attached []int64
	for i, st := range t.streams {
		f.wanted = append(f.wanted, st.key)
		tr := f.track(st.key)
		if tr.h != nil || tr.held && tr.row == rows[i].ID || tr.ended && tr.endedAt == st.gen {
			continue
		}
		if tr.row != rows[i].ID {
			// A hold was the instance before's, whose end was its log's.
			tr.row, tr.wm = rows[i].ID, f.start.keyFor(pageStream{row: rows[i]})
			tr.held, tr.restarted, tr.filling = false, false, false
		}
		tr.st, tr.ended, tr.refused = st, false, ""
		if st.finished() {
			drain = append(drain, tr)
			continue
		}
		if err := f.attach(ctx, tr); err != nil {
			return err
		}
		attached = append(attached, tr.row)
	}
	if err := f.drain(ctx, drain); err != nil {
		return err
	}
	// A commit before a new receiver reached only the last, so every stream is walked
	// then; otherwise only the ones just attached are new.
	walk := attached
	if resubscribed {
		walk = f.pingRows
	}
	if err := f.walkRows(ctx, walk); err != nil {
		return err
	}
	running, waiting := 0, t.starting
	for _, st := range t.streams {
		tr := f.streams[st.key]
		switch {
		case tr.h != nil:
			running++
		case tr.held, tr.filling:
			waiting = true
		case tr.ended && tr.endedAt == st.gen:
			// A container waiting to start has a log to come, whatever its last read
			// answered; a previous instance's never grows.
			waiting = waiting || !st.key.Previous && st.gen.state == stateWaiting
		}
	}
	if running == 0 && !waiting && len(t.streams) > 0 {
		return f.stopped(t.streams)
	}
	return nil
}

// subscribe makes the bus's receiver anew for ids, the rows of the streams the view
// wants and of every tailer the tail holds, sorted, unless it has them already, and
// is whether it did.
func (f *cursorTail) subscribe(ids []int64) (bool, error) {
	if f.pings != nil && slices.Equal(ids, f.pingRows) {
		return false, nil
	}
	pings, err := f.file.ListenForStreamChanges(ids...)
	if err != nil { // coverage:ignore only the file closing between filing the streams and subscribing
		return false, err
	}
	if f.pings != nil {
		f.pings.Close()
	}
	f.pings, f.pingRows = pings, ids
	return true, nil
}

// drain walks each of streams, finished instances, to its end, filling what the file
// lacks of it, and ends each at its generation unless its walk held it or left a
// fill in flight, whose end finishes the drain.
func (f *cursorTail) drain(ctx context.Context, streams []*tracked) error {
	if err := f.walkStreams(ctx, streams); err != nil {
		return err
	}
	for _, tr := range streams {
		f.drained(tr)
	}
	return nil
}

// drained ends tr, a finished instance walked to its end, at its generation unless
// it is held, ended already, or filling.
func (f *cursorTail) drained(tr *tracked) {
	if !tr.held && !tr.ended && !tr.filling {
		tr.ended, tr.endedAt = true, tr.st.gen
	}
}

// walkRows walks each stream the tail reads whose row is in rows.
func (f *cursorTail) walkRows(ctx context.Context, rows []int64) error {
	var streams []*tracked
	for _, key := range f.wanted {
		if tr := f.streams[key]; tr.h != nil && slices.Contains(rows, tr.row) {
			streams = append(streams, tr)
		}
	}
	return f.walkStreams(ctx, streams)
}

// walkStreams walks each of streams, one at a time, sends what they read in line
// order, and walks again at once each that stopped with more to read.
func (f *cursorTail) walkStreams(ctx context.Context, streams []*tracked) error {
	for len(streams) > 0 {
		f.lines, f.bytes = tailBatch, tailBatchBytes
		var again []*tracked
		for _, tr := range streams {
			more, err := f.walk(ctx, tr)
			if err != nil {
				return err
			}
			if more {
				again = append(again, tr)
			}
		}
		if !f.flush(ctx) {
			return nil
		}
		streams = again
	}
	return nil
}

// walk reads tr's stream from its watermark, in line order, as far as reach allows,
// keeping the lines the grep passes for the next send and moving the watermark to
// the last line it read. At a live gap it holds the watermark and starts a fill of
// the gap, then stops: the fill's end walks the stream again. A stream already
// filling is not walked. It answers whether it stopped on the send's allowance of
// lines or bytes with more to read.
func (f *cursorTail) walk(ctx context.Context, tr *tracked) (bool, error) {
	if tr.filling {
		return false, nil
	}
	// A fill that read the gap from the watermark lets the walk cross it, lines or
	// none. Its chunks are held, not stamped, until the walk has read them.
	crossed := tr.crossed
	if tr.release != nil {
		defer tr.release()
	}
	tr.crossed, tr.release = false, nil
	if f.lines <= 0 || f.bytes <= 0 {
		return true, nil
	}
	// Read before the snapshot, so the snapshot holds every line up to it.
	committed := tr.committed()
	var gap *logstore.Gap
	reach, more := tr.wm, false
	err := f.file.Read(ctx, func(r logstore.Reader) error {
		chunks, err := r.Chunks(tr.row, f.scope.Grep)
		if err != nil {
			return err
		}
		cov := logstore.CoverageOf(chunks, f.names, tr.tail())
		reach, gap = f.reach(cov, tr, crossed, committed)
		held := heldBy(chunks)
		upto, first := tr.wm, -1
		err = r.Lines(tr.row, tr.wm, reach, func(l logstore.Line) bool {
			tr.gap, upto = joined(tr.gap, holesBetween(cov.Gaps, committed, upto, l.Pos)), l.Pos
			if held(l.Pos) && logline.Matches(f.grep, l.Message) {
				rec := tr.record(l)
				rec.Gap = tr.gap
				if !tr.looked {
					first = len(f.batch)
				}
				tr.gap, tr.looked = nil, true
				f.batch = append(f.batch, rec)
				f.bytes -= len(l.Message)
			}
			if f.lines--; f.lines <= 0 || f.bytes <= 0 {
				reach, more = l.Pos, true
				return false
			}
			return true
		})
		if err != nil {
			return err
		}
		tr.gap = joined(tr.gap, holesBetween(cov.Gaps, committed, upto, reach))
		if first < 0 {
			return nil
		}
		// The tail may start past a hole no page carried: past the last line a
		// page sent, or a moment past the hole. A look its bytes cut keeps what the
		// walk crossed.
		l := looker{
			r: r, row: tr.row, cov: cov, committed: committed, held: held, keep: f.keep,
			bytes: f.s.files.Retention().LookBytes,
		}
		found, err := l.back(lineOf(f.batch[first]).Pos)
		f.batch[first].Gap = joined(f.batch[first].Gap, found)
		return err
	})
	if err != nil {
		return false, err
	}
	tr.wm = logstore.MaxPos(tr.wm, reach)
	if !more && gap != nil {
		f.fill(ctx, tr, *gap)
	}
	return more, nil
}

// reach is how far from tr's watermark a walk may read: along the spans of cov,
// across every gap no fill may be offered, a lost gap or its tailer's, and, once a
// fill read it, the gap the watermark sits in; and the live gap it stops at, nil for
// none. A span past a tailer's gap that the tailer has not committed up to is a
// page's, written ahead of it, and the lines before it are the tailer's, still to
// come: the walk stops at the gap and waits for the tailer's ping. committed is what
// the tailer had written before chunks were read.
func (f *cursorTail) reach(cov logstore.Coverage, tr *tracked, crossed bool, committed *logstore.Pos) (logstore.Pos, *logstore.Gap) {
	reach := tr.wm
	for i, seg := range segmentsFrom(cov, tr.wm, logline.Newer) {
		switch {
		case seg.span != nil:
			reach = seg.span.To
		case seg.gap.Live() && !(i == 0 && crossed):
			return reach, seg.gap
		case seg.gap.Tailed && seg.gap.Before != nil && committed.Compare(seg.gap.Before.Prev()) < 0:
			return reach, nil
		}
	}
	return reach, nil
}

// errRowGone is a gap fill whose stream row the janitor took: the stream's pod is
// gone, so the stream waits out its backoff until a reconcile drops it.
var errRowGone = errors.New("logs: the stream's row is gone from the log file")

// fillDone is the end of a gap fill of tr's stream: the row it filled, what it read
// (with whether the janitor took the row first), the hold on what it wrote, and its
// failure, fatal or the file's.
type fillDone struct {
	tr      *tracked
	row     int64
	out     streamFill
	release func()
	err     error
}

// fill starts a read of the live gap g of tr's stream from its watermark, as a newer
// page's fill reads it, under the stream's share of one query's limits, and the write
// of what it read. It runs on its own, so a slow read holds up no other stream, and
// its end comes back on filled. What it writes is unstamped, since a tail's lines
// are in the viewer's window, and held until the walk after it has read them.
func (f *cursorTail) fill(ctx context.Context, tr *tracked, g logstore.Gap) {
	st := tr.st // the fill's own copy: the tail's goroutine owns tr
	ps := tr.pageStream()
	ps.st = &st
	fl := &filler{
		file: f.file, clusters: f.s.clusters, id: f.id, ret: f.s.files.Retention(), ps: ps, backends: f.backends, names: f.names,
		q:      pageQuery{scope: f.scope, grep: f.grep, mode: logline.Newer, limit: MaxPageSize},
		limits: kubelet.FollowLimits(f.s.caps, len(f.wanted)),
	}
	wm := tr.wm
	tr.filling = true
	f.fills.Go(func() {
		d := f.readGap(ctx, fl, g, wm)
		d.tr = tr
		select {
		case f.filled <- d:
		case <-ctx.Done():
			d.release()
		}
	})
}

// readGap runs fl's read of g from wm, writing what it read, held. On an error it
// releases what it held, and the release it answers is a no-op.
func (f *cursorTail) readGap(ctx context.Context, fl *filler, g logstore.Gap, wm logstore.Pos) fillDone {
	var releases []func()
	fl.save = func(ctx context.Context, fills []logstore.Fill) (logstore.Written, error) {
		w, release, err := f.file.WriteHeld(ctx, fills)
		releases = append(releases, release)
		return w, err
	}
	release := func() {
		for _, r := range releases {
			r()
		}
	}
	d := fillDone{row: fl.ps.row.ID, release: func() {}}
	if _, d.err = fl.fillGap(ctx, g, wm, true); d.err != nil {
		release()
		return d
	}
	d.out, d.release = fl.out, release
	return d
}

// filledGap takes the end of a gap fill. A fill of a row the stream no longer reads
// is thrown away. One that read nothing lets the stream go, held or ended as its
// failure says; a failure fatal or of the file ends the tail. Otherwise the stream
// is walked on across the gap, unless it has left the view or lost its tailer in the
// meantime, when the next attach walks it from its watermark; a finished instance's
// walk to its end finishes its drain.
func (f *cursorTail) filledGap(ctx context.Context, d fillDone) error {
	tr := d.tr
	if tr.row != d.row {
		d.release()
		return nil
	}
	tr.filling = false
	// An untailed stream filling was judged waiting, so the view's end is judged again.
	f.rejudge = f.rejudge || tr.h == nil
	if d.err != nil {
		d.release()
		return d.err
	}
	if d.out.wrote && f.s.afterFill != nil {
		f.s.afterFill()
	}
	switch {
	case d.out.gone: // coverage:ignore only a sweep between filing the row and the tail holding it
		// The row went before the tail held it: its pod is gone.
		d.release()
		return f.failed(ctx, tr, errRowGone)
	case !d.out.fromKey:
		d.release()
		cause := d.out.err
		if cause == nil {
			// The connection's retirement cut the read before its first line.
			cause = kubelet.ErrRetired
		}
		return f.failed(ctx, tr, cause)
	case !slices.Contains(f.wanted, tr.st.key), tr.h == nil && !tr.st.finished():
		d.release()
		return nil
	}
	tr.crossed, tr.release = true, d.release
	if err := f.walkStreams(ctx, []*tracked{tr}); err != nil {
		return err
	}
	if tr.h == nil {
		f.drained(tr)
	}
	return nil
}

// flush sends the lines walked in line order. It is false once the tail's
// context has ended.
func (f *cursorTail) flush(ctx context.Context) bool {
	slices.SortStableFunc(f.batch, logline.CompareLines)
	for _, rec := range f.batch {
		if !f.sendLine(ctx, rec) {
			return false
		}
		f.streams[rec.Stream].drops = 0
	}
	f.batch = f.batch[:0]
	return true
}

// stopped is the ending of a tail whose every stream ended: the cluster's
// refusal when that is how one ended, since tailing again would end the same way.
func (f *cursorTail) stopped(streams []stream) error {
	for _, st := range streams {
		if tr := f.streams[st.key]; tr.refused != "" {
			return refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: "+tr.refused)
		}
	}
	return errStopped
}

// noteDeleted keeps the pod frame deletes when it is a live Pod source's.
func (f *cursorTail) noteDeleted(frame cluster.ClusterCachedDataObjectCollectionWatchFrame) {
	if frame.Type != cluster.CollectionWatchFrameDeleted || frame.Object == nil {
		return
	}
	for _, src := range f.scope.Sources {
		key := src.Namespace + "/" + src.Name
		if src.Kind == SourcePod && !f.gone[key] && src.Namespace == frame.Object.Namespace && src.Name == frame.Object.Name {
			f.deleted[key] = frame.Object.UID
			return
		}
	}
}

// resolve resolves the view less each Pod source whose pod is gone, and is false
// when the mirror is to be asked again. A resolve that finds a pod newly gone is
// run again without its source, since another source may read a pod of its name.
func (f *cursorTail) resolve(ctx context.Context) (target, bool, error) {
	for {
		t, err := resolve(ctx, f.s.clusters, f.id, "", f.live(), true)
		switch {
		case errors.Is(err, ErrNoCache), errors.Is(err, ErrSyncing), err == nil && t.noNodes != nil:
			f.askAgain()
			return target{}, false, nil
		case err != nil:
			return target{}, false, err
		case t.serverUID != f.target.serverUID:
			return target{}, false, kubelet.ErrRevoked
		}
		gone, err := f.podsGone(t)
		if err != nil || !gone {
			f.snapshot = false
			return t, err == nil, err
		}
	}
}

// live is the view less each Pod source whose pod is gone.
func (f *cursorTail) live() Scope {
	v := f.scope
	v.Sources = slices.DeleteFunc(slices.Clone(v.Sources), func(src ScopeSource) bool {
		return src.Kind == SourcePod && f.gone[src.Namespace+"/"+src.Name]
	})
	return v
}

// notePods keeps the UID of each Pod source's pod t holds, unless one is kept.
func (f *cursorTail) notePods(t target) {
	for key, uid := range t.pods {
		if f.podUIDs[key] == "" {
			f.podUIDs[key] = uid
		}
	}
}

// podsGone is whether t, resolved over the live view, shows a Pod source's pod
// newly gone: one deleted that t does not hold again by its UID, one whose name t
// holds under another UID than the first, or, after a snapshot, one t no longer
// holds. A view whose every source is a pod gone ends the tail; any other
// follows its other sources.
func (f *cursorTail) podsGone(t target) (bool, error) {
	newly := false
	for _, src := range f.scope.Sources {
		key := src.Namespace + "/" + src.Name
		if src.Kind != SourcePod || f.gone[key] {
			continue
		}
		uid, deleted := f.deleted[key]
		delete(f.deleted, key)
		held, first := t.pods[key], f.podUIDs[key]
		switch {
		case deleted && (held == "" || held != uid),
			first != "" && held != "" && held != first,
			first != "" && held == "" && f.snapshot:
			f.gone[key], newly = true, true
		}
	}
	f.notePods(t)
	var keys []string
	for _, src := range f.scope.Sources {
		key := src.Namespace + "/" + src.Name
		if src.Kind != SourcePod || !f.gone[key] {
			return newly, nil
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 1 {
		return true, refusal.New(refusal.CodeFinished, "pod "+keys[0]+" was deleted")
	}
	return true, refusal.New(refusal.CodeFinished, "pods "+strings.Join(keys, ", ")+" were deleted")
}

// ===== The tailer =====

// tailer is one stream's following read, writing what it reads into the file through the
// file's writer. Its fields past done are guarded by the registry's mu, but err, which
// the read's end sets before done closes.
type tailer struct {
	r      *registry
	st     stream
	row    int64
	cancel context.CancelFunc
	// ready closes once the start has run, read or failed, and done once the read has
	// ended, err then saying how.
	ready chan struct{}
	done  chan struct{}
	err   error

	refs     int
	stopping bool
	grace    *time.Timer
	// start is where the read reads on from: every gap past it is the tailer's. A walk
	// reads it holding one of the file's reader connections, so it takes no lock.
	start atomic.Pointer[logstore.Pos]
	// current is the file's tail the read writes into, nil until its first line.
	current atomic.Pointer[logstore.Tail]
}

// started is whether t's start has run; a tailer not started is being started.
func (t *tailer) started() bool {
	select {
	case <-t.ready:
		return true
	default:
		return false
	}
}

// committed is how far t has written into the file: the last line its tail has landed,
// or its start before that.
func (t *tailer) committed() logstore.Pos {
	p := *t.start.Load()
	if tail := t.current.Load(); tail != nil {
		if c := tail.Committed(); c != nil {
			p = logstore.MaxPos(p, *c)
		}
	}
	return p
}

// join holds t for one more tail, and keeps it past its grace. It is called with
// mu held.
func (t *tailer) join() {
	t.refs++
	if t.grace != nil {
		t.grace.Stop()
		t.grace = nil
	}
}

// tailRead is one following read and the moment its GET's first line came at, which is
// set before that line goes out.
type tailRead struct {
	read   *kubelet.Read
	oldest time.Time
}

// read opens a following read of the stream past start.
func (t *tailer) read(ctx context.Context, backend kubelet.Backend, start logstore.Pos) *tailRead {
	r := &tailRead{}
	r.read = backend.Forward(ctx, t.st.log(), kubelet.Options{
		Since:    logline.Bound{At: t.st.run().At(start), Inclusive: false},
		Follow:   true,
		OnOldest: func(ts time.Time) { r.oldest = ts },
	})
	return r
}

// shed ends a tailer that fell behind the writer while no tail holds it: nobody
// is reading it, so it stops competing for the writer rather than reading again. An
// attach that lands first keeps it reading; one after waits on done and starts a
// tailer afresh.
func (t *tailer) shed() bool {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	if t.refs > 0 {
		return false
	}
	t.stop()
	return true
}

// stop ends the tailer. It is called with mu held.
func (t *tailer) stop() {
	t.stopping = true
	t.cancel()
}

// run reads the stream until the read ends or the tailer stops, reading again from
// now whenever the writer falls behind while a tail holds it, and records how the
// read ended. release is the registry's hold on its stream's row, let go as it ends.
func (t *tailer) run(ctx context.Context, file *logstore.Store, backend kubelet.Backend, read *tailRead, prev *logstore.Pos, release func()) {
	defer func() {
		release()
		t.r.ended(t)
		close(t.done)
	}()
	defer t.cancel()
	for {
		behind, err := t.tail(ctx, file, read, prev)
		if !behind {
			t.err = err
			return
		}
		if t.shed() {
			return
		}
		// The lines it could not hand over are a live gap below the new start, which a
		// tail or a page that reaches them fills.
		start := logstore.Pos{TS: t.r.s.now().UnixNano(), Ord: -1}
		t.start.Store(&start)
		read, prev = t.read(ctx, backend, start), nil
	}
}

// tail takes tr's lines into the file, continuing the chunk ending at prev when it is
// set, and hands them to the writer in batches: whatever has arrived, up to
// WriteBatch lines or WriteBatchBytes. It answers whether it stopped because the
// writer could not take a batch within WriteWait, and otherwise how the read ended.
func (t *tailer) tail(ctx context.Context, file *logstore.Store, tr *tailRead, prev *logstore.Pos) (bool, error) {
	ret := t.r.s.files.Retention()
	read := tr.read
	defer drainReading(read)
	defer read.Cancel()
	var tail *logstore.Tail
	var failed <-chan struct{}
	ch := read.Lines.Chan()
	for {
		var rec kubelet.Item
		var ok bool
		select {
		case rec, ok = <-ch:
		case <-failed:
			// What the writer lost is a gap a fill reads once this tailer has ended.
			return false, t.close(tail, nil, tail.Err())
		}
		if !ok {
			return false, t.close(tail, nil, read.Err)
		}
		if tail == nil {
			// The first line has come, so the moment the read began at is known.
			var err error
			atBeginning := rotatedAway(tr.oldest, prev)
			if atBeginning {
				prev = nil
			}
			if tail, err = file.OpenTail(t.row, t.r.s.backends[0].name, prev, atBeginning); err != nil { // coverage:ignore only the file closing as the first line arrives; the close ends the read
				return false, err
			}
			t.current.Store(tail)
			failed = tail.Failed()
		}
		lines, open := gather(ch, ret, lineOf(rec.Record))
		if !open { // coverage:ignore only a read ending while gather takes what has arrived
			return false, t.close(tail, lines, read.Err)
		}
		wctx, cancel := context.WithTimeout(ctx, ret.WriteWait)
		err := tail.Write(wctx, lines)
		cancel()
		switch {
		case err == nil:
		case ctx.Err() != nil: // coverage:ignore only the tailer stopping while a batch waits on the writer
			return false, t.close(tail, nil, nil)
		case errors.Is(err, context.DeadlineExceeded):
			// Stop reading before waiting on the writer to close the chunk.
			read.Cancel()
			return true, t.close(tail, nil, nil)
		default: // coverage:ignore only a write failing between the Failed check and the send; Failed catches the rest
			return false, t.close(tail, nil, err)
		}
	}
}

// gather is first and whatever else has arrived on ch, up to WriteBatch lines or
// WriteBatchBytes, and whether ch is still open. A tail's read ends no window.
func gather(ch <-chan kubelet.Item, ret logstore.Retention, first logstore.Line) ([]logstore.Line, bool) {
	lines, bytes := []logstore.Line{first}, int64(len(first.Message))
	for len(lines) < ret.WriteBatch && bytes < ret.WriteBatchBytes {
		select {
		case rec, ok := <-ch:
			if !ok { // coverage:ignore only a read ending while gather takes what has arrived
				return lines, false
			}
			l := lineOf(rec.Record)
			lines, bytes = append(lines, l), bytes+int64(len(l.Message))
		default:
			return lines, true
		}
	}
	return lines, true
}

// lineOf is rec as the file stores it.
func lineOf(rec logline.Record) logstore.Line {
	return logstore.Line{Pos: logstore.Pos{TS: rec.Timestamp.UnixNano(), Ord: rec.Ordinal}, Message: rec.Message}
}

// close closes tail with its last lines, when it was opened, and answers the read's
// failure, else the close's.
func (t *tailer) close(tail *logstore.Tail, lines []logstore.Line, readErr error) error {
	if tail == nil {
		return readErr
	}
	// The tailer's own context may have ended; its chunk still has to close.
	err := tail.Close(context.Background(), lines)
	if readErr != nil {
		return readErr
	}
	return err
}

// ===== The registry =====

// registry is one log file's tailers: one per stream, shared by every tail that
// attaches to it and kept the service's grace after the last lets go. Every tailer is
// unlabelled, since the grep runs where the lines are read. While any runs, the
// registry holds a claim on the file and one on the cluster, which the tailers read
// over. It reconciles nothing: a tailer that ends is dropped, and the next attach
// starts one afresh. A registry lives as long as the service, since a tail holds
// its registry for its own life: one per cache id ever tailed.
type registry struct {
	s       *Service
	cacheID cluster.ClusterCacheID

	mu      sync.Mutex
	tailers map[int64]*tailer
	// id and serverUID are the cluster the file was resolved against, as the last
	// attach with no tailer running found it, since a context can be repointed.
	id        cluster.ClusterID
	serverUID string

	// claimMu guards file, claim and backend, held while any tailer runs or starts and
	// nil otherwise; a start takes them outside mu, since taking them is slow.
	claimMu sync.Mutex
	file    *logstore.Store
	claim   *kubelet.Claim
	backend kubelet.Backend
}

// registryFor is the registry of the file t was resolved against, on cluster id.
func (s *Service) registryFor(id cluster.ClusterID, t target) *registry {
	s.tailsMu.Lock()
	r := s.tails[t.cacheID]
	if r == nil {
		r = &registry{s: s, cacheID: t.cacheID, tailers: map[int64]*tailer{}}
		s.tails[t.cacheID] = r
	}
	s.tailsMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tailers) == 0 {
		r.id, r.serverUID = id, t.serverUID
	}
	return r
}

// tailOf is the start of the tailer running on the stream row of cacheID's file and
// how far it has written, both nil with none or before the tailer's start has run.
// Both are read under the one lock, so committed never runs ahead of the start it is
// read against.
func (s *Service) tailOf(cacheID cluster.ClusterCacheID, row int64) (start, committed *logstore.Pos) {
	s.tailsMu.Lock()
	r := s.tails[cacheID]
	s.tailsMu.Unlock()
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.tailers[row]
	if t == nil || t.stopping || t.start.Load() == nil {
		return nil, nil
	}
	c := t.committed()
	return t.start.Load(), &c
}

// attach joins the tailer of st, the stream row of the file, starting it when none
// runs at the later of the stream's newest line and watermark. Release the handle.
func (r *registry) attach(ctx context.Context, st stream, row int64, watermark logstore.Pos) (*handle, error) {
	for {
		r.mu.Lock()
		t := r.tailers[row]
		switch {
		case t == nil:
			t = &tailer{r: r, st: st, row: row, done: make(chan struct{}), ready: make(chan struct{}), refs: 1}
			r.tailers[row] = t
			r.mu.Unlock()
			if err := r.start(ctx, t, watermark); err != nil {
				return nil, err
			}
			return &handle{t: t}, nil
		case !t.started():
			// Another attach is starting it.
			r.mu.Unlock()
			select {
			case <-t.ready: // coverage:ignore only two attaches racing to start one tailer
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case !t.stopping:
			t.join()
			r.mu.Unlock()
			return &handle{t: t}, nil
		}
		// Its chunk closes as it ends; one started now would race it.
		r.mu.Unlock()
		select {
		case <-t.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// start runs t, in tailers and not yet ready, from the later of its stream's newest
// line and watermark, over the registry's claims, taken when none runs. It runs with
// mu released: t's place in tailers keeps the claims held meanwhile, and the other
// attaches wait on ready. A start that fails drops t, and gives the claims back when
// no tailer is left.
func (r *registry) start(ctx context.Context, t *tailer, watermark logstore.Pos) error {
	defer close(t.ready)
	err := r.run(ctx, t, watermark)
	if err != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.tailers, t.row)
		if len(r.tailers) == 0 {
			r.letGo()
		}
	}
	return err
}

// run takes the claims and starts t's read from the later of its stream's newest
// line and watermark.
func (r *registry) run(ctx context.Context, t *tailer, watermark logstore.Pos) error {
	if err := r.hold(ctx); err != nil {
		return err
	}
	chunks, err := r.file.Chunks(context.Background(), t.row, "")
	if err != nil {
		return err
	}
	start, prev := watermark, (*logstore.Pos)(nil)
	if newest := newestOf(chunks); newest != nil && newest.Compare(watermark) >= 0 {
		start, prev = *newest, newest
	}
	// A tailer in its grace has no tail to end it, so the file closing does.
	fileCtx, err := r.file.Context()
	if err != nil { // coverage:ignore only the file closing between the tailer's read of its chunks and this
		return err
	}
	ctx, t.cancel = context.WithCancel(fileCtx)
	t.start.Store(&start)
	file, backend := r.file, r.backend
	release := file.Hold(t.row)
	first := t.read(ctx, backend, start)
	go t.run(ctx, file, backend, first, prev, release)
	return nil
}

// hold takes the claims the tailers run over, unless they are held.
func (r *registry) hold(ctx context.Context) error {
	r.claimMu.Lock()
	defer r.claimMu.Unlock()
	if r.file != nil {
		return nil
	}
	file, err := r.s.files.OpenOrCreate(int64(r.cacheID))
	if err != nil {
		return err
	}
	c, err := kubelet.Open(ctx, r.s.clusters, r.id, r.serverUID, r.s.retry)
	if err != nil {
		file.Close()
		return err
	}
	r.file, r.claim, r.backend = file, c, r.s.backends[0].build(c.Get)
	return nil
}

// letGo gives back the claims once no tailer runs. It is called with mu held.
func (r *registry) letGo() {
	r.claimMu.Lock()
	defer r.claimMu.Unlock()
	if r.file == nil {
		return
	}
	r.claim.Close()
	_ = r.file.Close() // the claim's file closing is no failure of anything still running
	r.file, r.claim, r.backend = nil, nil, nil
}

// release lets go of one hold on t, which stops once its grace has passed with none.
func (r *registry) release(t *tailer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t.refs--
	if t.refs > 0 || t.stopping {
		return
	}
	if r.s.grace <= 0 {
		t.stop()
		return
	}
	t.grace = time.AfterFunc(r.s.grace, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if t.refs == 0 && !t.stopping {
			t.stop()
		}
	})
}

// ended drops t, which has ended, and lets go of the claims once no tailer runs.
func (r *registry) ended(t *tailer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Ended in its grace, the timer would fire on a tailer the registry has dropped.
	if t.grace != nil {
		t.grace.Stop()
		t.grace = nil
	}
	if r.tailers[t.row] == t {
		delete(r.tailers, t.row)
	}
	if len(r.tailers) == 0 {
		r.letGo()
	}
}

// handle is one tail's hold on a tailer.
type handle struct {
	t    *tailer
	once sync.Once
}

// Done closes once the tailer has ended, its last lines in the file; Err is then
// how its read ended.
func (h *handle) Done() <-chan struct{} { return h.t.done }
func (h *handle) Err() error            { return h.t.err }

// Start is where the tailer reads on from now: every gap of its stream past it is
// the tailer's.
func (h *handle) Start() logstore.Pos { return *h.t.start.Load() }

// Committed is how far the tailer has written into the file: the last line its tail
// has landed, or its start before that. It holds once the tailer has ended.
func (h *handle) Committed() logstore.Pos { return h.t.committed() }

// Release lets go of the hold; a second call does nothing.
func (h *handle) Release() { h.once.Do(func() { h.t.r.release(h.t) }) }

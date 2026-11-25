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

// One kind's sync, as a worker on the supervisor. A run is the stream's whole life — the gate,
// then a cold LIST or a resume from the cookie, then the WATCH and the delta loop — and returning
// is the stream having ended. The supervisor owns what happens next: a rotation waits out the
// floor, a failure climbs the ladder, and an end nobody asked for that never proved itself live
// is NeverReady.
//
// → docs/adr/2026-08-28-jobs-and-workers.md.
package kubesync

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// nameKindSync is the kind worker's registration name on the kind supervisor. It is the only
// registration there: a kind is a subject, not a registration.
const nameKindSync = "sync"

// kindSubject names one kind on one cache, "<cacheID>/<apiVersion>/<resource>", and
// parseKindSubject reads it back. apiVersion carries its own / for a group, so the name is parsed
// as first segment, last segment, and everything between.
func kindSubject(cacheID int64, k kubestore.Kind) string {
	return fmt.Sprintf("%d/%s/%s", cacheID, k.APIVersion, k.Resource)
}

func parseKindSubject(subject string) (int64, kindID, bool) {
	cachePart, rest, ok := strings.Cut(subject, "/")
	if !ok {
		return 0, kindID{}, false
	}
	i := strings.LastIndex(rest, "/")
	if i < 0 {
		return 0, kindID{}, false
	}
	cacheID, err := strconv.ParseInt(cachePart, 10, 64)
	if err != nil {
		return 0, kindID{}, false
	}
	return cacheID, kindID{apiVersion: rest[:i], resource: rest[i+1:]}, true
}

// pacing is every duration and bound a kind sync runs on. A test shrinks them; production
// passes defaultPacing, so no test outwaits a production number.
type pacing struct {
	// staleAfter is how long a watch may go without a frame before it is reopened from the
	// cookie, and how long an open may take before its verdict says Resuming.
	staleAfter time.Duration
	// bookmarkStaleAfter is how long a stream that has sent a bookmark may go without a frame
	// before it reads Stale: the apiserver's watch cache bookmarks about once a minute, so a
	// few missed in a row. Under staleAfter, or the reopen would always come first.
	bookmarkStaleAfter time.Duration
	// backoff is the ladder a failing run climbs — the supervisor's, so a kind's retry countdown
	// reads the same as a sweep's. It is also the floor under a clean restart, which is what
	// stops a server that closes every watch after one frame reopening in a tight loop.
	backoff supervisor.Backoff
	// pageSize bounds one relist page, so a large collection never lands in memory whole.
	pageSize int64
	// kindStartConcurrency bounds the kind syncs STARTING at once across every cache, and so
	// bounds the cold lists: a worker holds a slot only until its first frame, so this is the
	// gate arming a cache of hundreds of kinds needs, however many are already streaming.
	kindStartConcurrency int
}

func defaultPacing() pacing {
	return pacing{
		staleAfter:           5 * time.Minute,
		bookmarkStaleAfter:   3 * time.Minute,
		backoff:              supervisor.Backoff{Base: time.Second, Factor: 2, Cap: time.Minute},
		pageSize:             500,
		kindStartConcurrency: 16,
	}
}

// kindSync is the worker every kind subject runs. It holds nothing per kind: the subject names
// one, and enterKindRun hands the run the whole value.
type kindSync struct {
	s      *Service
	pacing pacing
}

// Run is one kind's stream, start to finish. It blocks for the stream's whole life, which is what
// makes it a worker: there is no goroutine here outliving the call, and nothing to hand back.
func (w kindSync) Run(ctx context.Context, pass *supervisor.WorkerPass[Reason]) supervisor.Result {
	cacheID, id, ok := parseKindSubject(pass.Subject())
	if !ok {
		return supervisor.Skip()
	}

	sess, k, ok := w.s.enterKindRun(cacheID, id)
	if !ok {
		// Nothing has armed this cache, its teardown has begun, or nothing tracks this kind:
		// either way the claims a run would write through are going.
		return supervisor.Skip()
	}
	defer sess.leaveRun()

	// The session's cancel is the backstop behind the supervisor's own: a teardown ends every
	// stream under the cache without having to remove each subject first.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(sess.ctx, cancel)()

	return w.sync(ctx, sess, k, id, pass)
}

// sync gates, establishes, and then streams. Everything that can refuse is answered here: the
// identity gate suspends, a list or an open that fails climbs the ladder, and an end nobody asked
// for that reached no frame is the supervisor's NeverReady.
func (w kindSync) sync(ctx context.Context, sess *session, k kubestore.Kind, id kindID, pass *supervisor.WorkerPass[Reason]) supervisor.Result {
	conn, err := sess.connFor(ctx, kindGateKey(id), ReasonSyncFailed)
	if err != nil {
		// Nothing syncs into a cache whose connection does not vouch for its ServerUID. The
		// worker records why and parks; the session's connection bridge is what wakes it.
		return supervisor.Suspend(connectionReason(err, ReasonSyncFailed), err.Error())
	}
	// The stream lasts as long as the connection it was opened over: a request served over a
	// retired one is one nothing will answer, and the restart goes back through the gate to
	// find the replacement.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Done is a channel rather than a context, so the watcher is a goroutine of its own; it
	// ends with the run either way.
	go func() {
		select {
		case <-conn.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	syncer := &kindSyncer{
		kind:   k,
		conn:   conn,
		store:  sess.store,
		pacing: w.pacing,
		sess:   sess,
		id:     id,
		pass:   pass,
		reason: pass.Prev(),
		relist: sess.needsRelist(id),
	}

	watcher, err := syncer.open(ctx)
	if err != nil {
		// **The one error a resume cannot retry its way out of**, since the cookie is what it
		// would retry with. Recorded on the session, which outlives this run and every failure
		// between learning the position is dead and acting on it.
		if positionGone(err) {
			sess.markRelist(id)
		}
		if ended(ctx, conn) {
			return w.stopped(pass, conn)
		}
		return supervisor.Fail(ReasonSyncFailed, err)
	}

	// **The starting phase ends here, not at the first frame.** The cold list and the open are
	// what cost; a watch on a quiet collection is up, not starting. Waiting for a frame would
	// hold this kind's start slot for as long as the collection stayed silent — bookmarks are
	// advisory — and the first few kinds would keep the rest of the cache from ever listing.
	pass.Ready()

	err = syncer.stream(ctx, watcher)
	if positionGone(err) {
		sess.markRelist(id)
	}
	switch {
	case ended(ctx, conn):
		return w.stopped(pass, conn)
	case err == nil && syncer.proved:
		// The apiserver ended the watch on its own timeout: a rotation, not a failure. The
		// rows stayed current across it, so the floor paces the reopen and the verdict stands.
		return supervisor.Succeeded()
	case err == nil:
		// It closed having told us nothing at all. Read as a rotation this would reopen at the
		// floor forever against a server that accepts every watch and drops it, and report
		// Watching the whole time; the ladder is the honest pacing for a stream we cannot say
		// works.
		return supervisor.Fail(ReasonSyncFailed, errWatchProvedNothing)
	default:
		return supervisor.Fail(ReasonSyncFailed, err)
	}
}

// errQuietWindow is a watch that went a whole window without a frame. It ends the stream but not
// the run, which reopens from the cookie: a quiet collection sends nothing, and one the server
// keeps out of its watch cache (core events, by default) sends no bookmarks either, so a window
// stood open is the only proof such a stream can get.
var errQuietWindow = errors.New("the watch was quiet for a whole window")

// errWatchProvedNothing is a watch the server accepted and closed without a frame, a bookmark, or
// even standing open long enough to go stale.
var errWatchProvedNothing = errors.New("the watch closed without proving itself")

// stopped is the exit for a run whose context ended: the session went, the supervisor stopped it,
// or the connection was retired under it. None is this kind's failure to report, so nothing is
// recorded — which is also why every path out of a cancel comes through here rather than deciding
// for itself.
//
// **A retirement still has to ask for its own next run.** The pool publishes the replacement
// before the run can notice the connection under it died, so the bridge's wake has already been
// and no other is owed. The wake lands on a key this run still holds, which the queue redelivers
// when it ends.
func (w kindSync) stopped(pass *supervisor.WorkerPass[Reason], conn *kubeconn.Connection) supervisor.Result {
	if retired(conn) {
		w.s.kindSupervisor.Wake(pass.Subject(), nameKindSync)
	}
	return supervisor.Skip()
}

// ended reports a run that something outside this kind ended: the session went, the supervisor
// stopped it, or the pool retired the connection under it.
//
// The retirement is checked directly rather than through the context, because Retire closes
// Done synchronously while the cancel it triggers waits on a goroutine. A run can therefore read
// an error off a connection nothing will answer with ctx.Err() still nil, and reporting that as
// the kind's own failure would put it on the ladder.
func ended(ctx context.Context, conn *kubeconn.Connection) bool {
	return ctx.Err() != nil || retired(conn)
}

// retired reports whether the pool took the connection this run was reading over.
func retired(conn *kubeconn.Connection) bool {
	select {
	case <-conn.Done():
		return true
	default:
		return false
	}
}

// positionGone reports the one error a resume cannot retry its way out of: the cookie is what it
// would retry with, and the server no longer serves from there.
func positionGone(err error) bool {
	return apierrors.IsResourceExpired(err) || apierrors.IsGone(err)
}

// kindSyncer is one run's state: the cold list or resume it performs, then the delta loop. Built
// fresh per run, so what must survive one — the verdict and the stamps — is read back off the
// pass and the session rather than held here.
type kindSyncer struct {
	kind   kubestore.Kind
	conn   *kubeconn.Connection
	store  *kubestore.Store
	pacing pacing

	// sess and id are where the per-frame stamps go; pass is where the reason goes and how the
	// run says it is up.
	sess *session
	id   kindID
	pass *supervisor.WorkerPass[Reason]

	// reason is this kind's standing answer, seeded from what the run before it committed —
	// which is what lets a resume hold its reason rather than walking through Syncing.
	reason Reason
	// proved marks the current stream as having shown it works: a frame, or simply staying
	// open past the window its rows are current for. Reset per stream, since a quiet watch's
	// replacement must prove itself. A plain field because one goroutine runs the whole sync.
	proved bool
	// bookmarked marks the current stream as having sent a bookmark, so its silence is
	// evidence: only such a stream reads Stale. Reset per stream, since its replacement may be
	// served by an apiserver that sends none.
	bookmarked bool
	// relist forces this run to cold-list, for the one failure a resume cannot retry its way
	// out of: the position the cookie names is the position the server dropped.
	relist bool
}

// open brings one kind up to date and hands back the watch it is current at. A cookie means a
// completed LIST is on disk, so the watch resumes from it; without one the collection is
// cold-listed first.
func (ks *kindSyncer) open(ctx context.Context) (watch.Interface, error) {
	k := ks.kind
	cookie, resuming, err := ks.store.Cookie(ctx, k.APIVersion, k.Resource)
	if err != nil {
		return nil, err
	}

	if resuming && !ks.relist {
		// A resume holds its reason: this kind has its rows, and walking every one of them
		// through Syncing on a resume poke is what makes a laptop opening cost a reconcile
		// per kind. Only a resume slow enough to have stopped being current says so, which
		// openWatch announces.
		return ks.openWatch(ctx, cookie, ReasonResuming)
	}
	// Both starts below take a full list, and they are not the same news: a cache holding
	// rows serves them throughout, where a cold one has nothing. The relist is what clears
	// the cookie, so a run that dies mid-page leaves none standing and the next one
	// reconciles rather than resuming over the gap.
	reason := Reason(ReasonSyncing)
	if ks.relist {
		reason = ReasonResyncing
	}
	ks.report(reason)
	if cookie, err = ks.coldList(ctx); err != nil {
		return nil, err
	}
	// Only now: the cookie survives a list that failed before its first page, so an intent
	// dropped any earlier would send the next run back to a position the server has already
	// refused, and it would alternate between a doomed watch and this list.
	ks.relist = false
	ks.sess.clearRelist(ks.id)
	return ks.openWatch(ctx, cookie, "")
}

// stream applies deltas across quiet windows, reopening each from the cookie, until a stream
// ends in anything else.
//
// **A reopen the server refuses as expired relists in place.** A collection sending no bookmarks
// leaves the cookie where the last frame put it, and etcd compacts on the same five minutes the
// window runs, so on a quiet cluster the cookie has often been compacted by the reopen. That is
// the routine end of a quiet window rather than the kind failing, and client-go's reflector
// answers it the same way. Only a reopen's refusal is read so: the same 410 on a stream just
// listed is a server that cannot serve its own position, and fails the run.
func (ks *kindSyncer) stream(ctx context.Context, watcher watch.Interface) error {
	reopened := false
	for {
		err := ks.applyDeltas(ctx, watcher)
		switch {
		case errors.Is(err, errQuietWindow):
			watcher, err = ks.reopen(ctx)
			reopened = true
		case !reopened || !positionGone(err):
			return err
		}
		// Refused on the open or on the stream it opened: either way, a reopen's.
		if positionGone(err) {
			watcher, err = ks.relistInPlace(ctx)
			reopened = false
		}
		if err != nil {
			return err
		}
	}
}

// relistInPlace cold-lists within the run, after pass.Ready, so it takes no start slot: only a
// collection the server serves without bookmarks reaches it, one list per quiet window.
func (ks *kindSyncer) relistInPlace(ctx context.Context) (watch.Interface, error) {
	// Marked on the session as well, so a list that fails is retried as a list by the next run
	// rather than as a resume from the position just refused.
	ks.sess.markRelist(ks.id)
	ks.relist = true
	return ks.open(ctx)
}

// reopen resumes a quiet watch from the cookie. An open that hangs past the window reads
// Resuming, as a slow resume does: the connection's pings fail a dead one's streams long before
// then, so a hang over a live one is a slow server. A stream already Stale has nothing to
// announce, since the hang proves nothing its silence did not.
func (ks *kindSyncer) reopen(ctx context.Context) (watch.Interface, error) {
	k := ks.kind
	cookie, _, err := ks.store.Cookie(ctx, k.APIVersion, k.Resource)
	if err != nil {
		return nil, err
	}
	if ks.reason == ReasonStale {
		return ks.openWatch(ctx, cookie, "")
	}
	return ks.openWatch(ctx, cookie, ReasonResuming)
}

// coldList replaces this kind's rows from a paginated LIST and returns the position a watch
// resumes from. The supervisor's start cap is what stops an armed cache putting hundreds of these
// on the connection and the store's single writer at once.
func (ks *kindSyncer) coldList(ctx context.Context) (string, error) {
	replace, err := ks.store.BeginReplace(ks.kind)
	if err != nil { // coverage:ignore open read the cookie off this store just before, so only a removal landing between fails it
		return "", err
	}

	var cookie, next string
	for {
		page, err := ks.collection().List(ctx, metav1.ListOptions{Limit: ks.pacing.pageSize, Continue: next})
		if err != nil {
			return "", fmt.Errorf("list %s: %w", ks.kind.Resource, err)
		}
		items := make([]*unstructured.Unstructured, len(page.Items))
		for i := range page.Items {
			items[i] = &page.Items[i]
		}
		if err := replace.WritePage(ctx, items); err != nil {
			return "", err
		}
		// Every page of one LIST answers at the same position, so the last page's is the
		// snapshot's — and a later page is what a partial pass would be missing.
		cookie, next = page.GetResourceVersion(), page.GetContinue()
		if next == "" {
			break
		}
	}
	if _, err := replace.Commit(ctx, cookie); err != nil {
		return "", err
	}
	return cookie, nil
}

// openWatch establishes the stream, reporting slow if the open drags past the window its rows stay
// current for; an empty slow announces nothing.
//
// The announcement is made from this goroutine, never a timer callback: one kind's verdict has
// one writer, and a callback firing as the stream settles would leave its reason standing over
// the Watching it raced — Stop does not wait for one already running.
func (ks *kindSyncer) openWatch(ctx context.Context, cookie string, slow Reason) (watch.Interface, error) {
	options := metav1.ListOptions{
		ResourceVersion: cookie,
		// Bookmarks are how an idle collection proves its watch is alive rather than wedged.
		AllowWatchBookmarks: true,
	}
	if slow == "" {
		return ks.collection().Watch(ctx, options)
	}

	// Buffered, so the open never blocks on a caller that has stopped waiting.
	done := make(chan opened, 1)
	go func() {
		watcher, err := ks.collection().Watch(ctx, options)
		done <- opened{watcher, err}
	}()

	select {
	case o := <-done:
		return o.watcher, o.err
	case <-ctx.Done():
		return nil, abandon(ctx, done)
	case <-time.After(ks.pacing.staleAfter):
		ks.report(slow)
	}

	// Still on ctx after the announcement: forgetting a kind joins its worker, and a run parked
	// on an open that will not unwind is one nothing can join.
	select {
	case o := <-done:
		return o.watcher, o.err
	case <-ctx.Done():
		return nil, abandon(ctx, done)
	}
}

// opened is what one watch open answered with.
type opened struct {
	watcher watch.Interface
	err     error
}

// abandon leaves an open that outlived its run to finish into nothing. It has to be collected:
// a watch that lands after the run is gone holds a connection nobody will close.
func abandon(ctx context.Context, done <-chan opened) error {
	go func() {
		if o := <-done; o.watcher != nil {
			o.watcher.Stop()
		}
	}()
	return ctx.Err()
}

// applyDeltas applies the watch's events until it ends or the run does. Returning nil is the
// apiserver ending the watch on its own timeout — a rotation, which the caller reads against
// ctx to tell from a stop — and errQuietWindow a whole window with no frame.
func (ks *kindSyncer) applyDeltas(ctx context.Context, watcher watch.Interface) error {
	defer watcher.Stop()

	opened := time.Now()
	ks.proved = false
	ks.bookmarked = false
	ks.report(ReasonWatching)

	quiet := time.NewTimer(ks.pacing.staleAfter)
	defer quiet.Stop()
	stale := time.NewTimer(ks.pacing.bookmarkStaleAfter)
	defer stale.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-quiet.C:
			// Standing open a whole window counts as proof the watch itself works: a server
			// that takes one and drops it never gets this far. A stream with no frame is also
			// proof its rows were current when it opened, since a position the server no longer
			// serves is refused on the stream, and nothing arrived after it. A stream with
			// frames has stamped them already.
			if !ks.proved {
				ks.sess.stampLive(ks.id, opened)
			}
			ks.proved = true
			return errQuietWindow
		case <-stale.C:
			// Silence is evidence only from a stream that has shown it bookmarks.
			if ks.bookmarked {
				ks.report(ReasonStale)
			}
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			if err := ks.apply(ctx, event); err != nil {
				return err
			}
			quiet.Reset(ks.pacing.staleAfter)
			stale.Reset(ks.pacing.bookmarkStaleAfter)
		}
	}
}

// apply lands one watch event. A bookmark carries no object of ours — it exists to move the
// position and prove the stream alive — so it books progress and nothing else.
func (ks *kindSyncer) apply(ctx context.Context, event watch.Event) error {
	if event.Type == watch.Error {
		// Wrapped, not flattened: whether the server dropped the position this stream reads
		// from is what decides between resuming and relisting.
		return fmt.Errorf("watch %s: %w", ks.kind.Resource, apierrors.FromObject(event.Object))
	}
	object, ok := event.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("watch %s: unexpected %T", ks.kind.Resource, event.Object)
	}

	k := ks.kind
	if event.Type == watch.Bookmark {
		if err := ks.store.SetCookie(ctx, k.APIVersion, k.Resource, object.GetResourceVersion()); err != nil {
			return err
		}
		ks.proved = true
		ks.bookmarked = true
		ks.sess.stampLive(ks.id, time.Now())
		ks.report(ReasonWatching)
		return nil
	}

	// The row and the position that would replay it land together, so no restart resumes
	// from a position the rows do not back.
	if err := ks.store.ApplyChange(ctx, k, event.Type, object); err != nil {
		return err
	}
	ks.proved = true
	ks.sess.stampUpdate(ks.id)
	ks.report(ReasonWatching)
	return nil
}

// report publishes this kind's answer, **only when it moved**: the reason is the worker's value,
// so every commit takes the supervisor's lock and fires a pass, and one per frame would publish
// per object on a busy cluster.
//
// Committing exactly on a change is also what lets the supervisor date it. A worker's ChangedAt is
// when its value last moved, which is what "watching since 10:02" reads off — so a commit that
// said nothing new would reset the stamp a reader is reading.
func (ks *kindSyncer) report(reason Reason) {
	if ks.reason == reason {
		return
	}
	ks.reason = reason
	ks.pass.Commit(reason)
}

// collection is this kind's rows across every namespace: a kind sync is the whole collection,
// and the cluster-scoped case is the same call without a namespace.
func (ks *kindSyncer) collection() dynamic.ResourceInterface {
	return ks.conn.Dynamic.Resource(gvrOf(ks.kind))
}

func gvrOf(k kubestore.Kind) schema.GroupVersionResource {
	gv, err := schema.ParseGroupVersion(k.APIVersion)
	if err != nil {
		// A kind reaches here from kind_catalog, whose rows are group-versions the server
		// itself named, so this is unparseable only if the table is corrupt.
		return schema.GroupVersionResource{Resource: k.Resource}
	}
	return gv.WithResource(k.Resource)
}

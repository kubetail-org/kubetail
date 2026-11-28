# TODO

Pending work across the three parts of the app. Grouped by area; detailed items keep their acceptance notes inline.

> **The specs are the plan.** Work with a settled shape has a spec — proposed in a pull request titled `📜 Spec: …`, or accepted on a `wip/<topic>` branch — and is not repeated here. This file holds what has no spec yet: watch items, simplifications, and work whose shape is still a question.

## Sidecar — cluster service

- **The cluster controller re-derives its source edge every pass.** `clusterController.Reconcile` (`sidecar/internal/services/cluster/clusters.go`) declares the dependency onto the kubeconfig anchor by looking the anchor up by name and calling `AddDependency` on every reconcile: a `GetByName` row read plus an `Edges().Add` transaction (a `SELECT` join and an `INSERT ... ON CONFLICT DO NOTHING`), whether or not the edge already exists. **Not a performance problem** — both are indexed round-trips, the expensive half (the requeue signal) is already gated on `ReconcileOwedStamped` so it fires once per edge ever created, and passes are paced by `clusterProbeInterval` (5m) plus events. Listed for the **simplification**, and because the cost is worth knowing before anyone puts this pass on a hot path.
  - **Fix:** own the edge instead of re-deriving it. The mirror (`mirror.go`) creates the runtime object, so it would create it with `beehive.WithOwner(...)` onto the source's anchor and let the pass read `client.GetOwner(ctx)`, exactly as `cacheController` already does. That deletes the `sourceClient` read, the `ErrNotFound` startup-requeue branch, and the `kubeconfigContextOf` special case, and a future non-kubeconfig source inherits the wake instead of needing its own branch. Store round-trips stay at two (`GetOwner` is also a read), so this buys clarity, not speed.
  - **Check before doing it:** every installed `beehive.db` holds Clusters created without the owner edge, so `GetOwner` reports no owner for them and they would never get the dependency. The change has to give those objects the owner (the mirror, on its first pass after the upgrade) or keep the name lookup for an object with none; that is why it is not purely mechanical. Also confirm `WithOwner` does not change Cluster lifecycle: owner edges cascade on delete, and nothing deletes an anchor today, so the risk is latent rather than live.
  - **Fix the stale comment either way:** the block's comment claims "every later pass is free", which is only true of the *wake*. The edge upsert still costs a transaction per pass.

- **`Cluster.caches` is an N+1 now that `ListByCluster` answers.** A query selecting it calls the resolver once per cluster, and each call is a join query plus a batched owner-edge read; the store is single-connection, so they serialize. Harmless at present row counts (tens), and the fix is a gqlgen dataloader rather than anything local — a per-resolver `List`-and-bucket would be worse, since the resolver still runs per cluster. **Trigger:** the first surface that selects `caches` over the whole fleet, or the first report of a slow `clusters { caches }`.

- **Nothing exposes the four non-connection probes, so "Connected but not Identified" has no detail.**
  `kubeconn.State` carries a full `Observation` per probe — value, `LastSeen`, `LastAttempt`
  (verdict/reason/message), `Failures`/`FailingSince`, `NextAttempt` — and `foldState`
  (`sidecar/internal/services/cluster/clusters.go`) deliberately copies only the **values** into
  `ClusterStatus`, since a status that moved every pass would re-emit the record to every watcher on
  every cycle. So a cluster whose credentials cannot read `kube-system` surfaces as one condition
  reason (`ReasonUIDUnreadable`) and nothing else: no failure count, no "next attempt in 4m", no
  distinction between the serverUID probe failing and the principal probe failing.
  **Shape to build:** a per-probe row — name, `lastAttempt {reason, message, at}`, failures /
  failing-since, next attempt, in flight — on its own subscription (a state watch, like the schedule: it
  moves after the record settles, so it must not be a field on `Cluster`; see the state watch bullet in
  `sidecar/CLAUDE.md`). **Note what it subsumes:** `Schedule.probing` becomes a field on the
  connection's row, read off the same snapshot. **Weigh:** five bare `nextAttemptAt`s would be no
  more useful than the one `clusterScheduleStateWatch` already serves — the reason/failure detail is
  the whole point, so build the row or build nothing.

- **`classify` has no `*apierrors.StatusError` branch.** Every probe reads a raw path today, so a
  status code is the whole evidence and `statusReason` covers it. **Trigger:** the first probe that
  goes through `Connection.Dynamic`, which returns the API's own reason — classifying that on the
  status code alone discards what the typed half already knows (`state.go` states the rule).
  `kubesync` already uses `Dynamic`, but no probe does, so nothing has reached it yet.

- **`supervisor.Supervisor`'s `Wake`/`WakeAll` pair reads as one axis and is two.** `Wake(subjectName string, names ...string)` takes named probes on **one** subject; `WakeAll(names ...string)` takes named probes across **every** subject. The variadic means the same thing in both, and `All` varies the argument that is not there — so `WakeAll` reads as "wake every probe" when it means "wake these probes everywhere". Both call sites are correct today: `watchKubeconfig` wants `WakeAll(nameConnection)` (one probe, whole fleet) and `RetryAndWait` wants `Wake(contextName, probeNames[:]...)` (one context, every probe) — they are exact transposes, which is what makes the pair easy to reach for backwards. **Fix:** rename `WakeAll` to name its axis (`WakeEverySubject`, or `WakeSubjects`), two call sites plus `engine_test.go` and the `sidecar/CLAUDE.md` wiring line. **Weigh:** the engine is a general leaf and `WakeAll` is the shorter, more conventional spelling; the case for renaming rests on the pair being read together, which is exactly when the ambiguity bites.

- **`supervisor.Supervisor`'s run queue has no debounce.** `runQ` and `passQ` are `internal/lib/workqueue`
  queues (`sidecar/internal/lib/supervisor/supervisor.go`). **Deduping is not debouncing** — a key waits once,
  but only while it is waiting, and one added while a worker holds it is queued afresh on `Done`,
  so asks spread across a run are a run apiece. For the connection probe each one re-reads the
  kubeconfig and the CA files behind it, then dials `/api`.
  - **Four producers reach one context's key today**: `Acquire` → `engine.Add` (once, per new
    context), `watchKubeconfig` → `WakeAll(nameConnection)` (every claimed context, per kubeconfig
    change), `RetryAndWait` → `Wake` (all five probes, on demand), and the engine itself — the data edge
    on a committed value, and the subject timer arming the next due pass.
  - **Not a problem yet**, and one throttle already exists: `WithWorkers` caps runs in flight
    fleet-wide, which is what holds back the first pass over a large kubeconfig so every cluster's
    credential helper does not run in the same second. It bounds concurrency, not the number of
    runs. The producers are also quiet — a claim asks once per new context, and `kubeconfig.Service`
    polls on a ticker and publishes only when the whole loaded config differs (`reflect.DeepEqual`),
    so a hand-edited file produces one signal rather than one per write.
  - **The trigger has partly fired.** `RetryAndWait` is the third producer and the first
    *user-driven* one: a client that retries on a timer, or a user clicking through an outage, asks
    for one context repeatedly, and dedup merges those asks only while the key is waiting. Bounded
    by more than a person's click rate now — the mutation is held open for the probe's round trip
    and the button is disabled for all of it — so this is a watch item rather than work, until a
    producer that can ask in a loop makes it real.
  - **Home:** `internal/lib/workqueue`, as its own feature. The old pairing with `AddAfter` is gone:
    the engine schedules delayed work with a per-subject `time.AfterFunc` over a schedule derived
    in `pass`, so nothing wants a delayed queue add any more.
    [amorey/gobus#17](https://github.com/amorey/gobus/issues/17) proposes the same queue upstream;
    this package is the worked shape to draw from if it lands.
  - **Weigh the alternative first:** a floor belongs to whoever knows what a run costs, and that is
    the engine (it owns the cadence, the backoff, and `WithWorkers`) rather than a generic queue. A
    per-probe minimum interval — "this probe runs at most once every N" — would cover the same
    burst without a second timing mechanism in the queue.

- **The cold-list burst sets the heap's high-water mark.** `defaultPacing`
  (`sidecar/internal/services/cluster/internal/kubesync/kinds.go`) is `pageSize: 500` against
  `kindStartConcurrency: 16`, so up to 8000 objects can be decoded at once — and each is live three
  times over at write time: client-go buffers the whole page body, decodes it into a
  `map[string]interface{}` several times its JSON size, and `projectObject` deep-copies that again
  through `sanitize` before marshalling the copy. Go sizes the heap off the peak and returns it
  slowly, so a burst lasting seconds costs resident memory for as long as the process runs. Both
  numbers are already `pacing` fields, so shrinking them is a line — the question is what arming
  latency they buy back.
  - **The deep copy is owed on one path only.** `sanitize` (`kubestore/objects.go`) copies because
    "the caller's body is the live watch object" — true of `ApplyChange`, false of a
    `ReplaceSession` page item, which is a fresh decode nothing else holds. Sanitizing a relist item
    in place would halve the per-object peak exactly where the peak is. It needs `extractStatus`
    hoisted above the strip first, since that deliberately reads the unsanitized body.
  - **Measure before tuning.** This is read off allocation shape, not a profile — it is the first
    thing the pprof item under *Sidecar (Go)* should confirm or kill.

- **Five SQLite pragmas nobody has measured.** The table shapes and the open contract are
  settled — every table is `STRICT`, the small keyed tables are `WITHOUT ROWID`, the three
  rowid tables each say in `0001_init.sql` why they are one, and `sqlitepool`
  is the single home for the writer/reader DSNs. What has never been looked at is
  `PRAGMA optimize` on close (it is what keeps the query planner's stats honest as a cache
  fills), WAL checkpoint/truncate behaviour under a long-lived writer, `page_size` against
  the compressed-body row width, `mmap_size` for the cached-data read path, and `cache_size`. **The
  instrument for the WAL question already exists**: `Stats` measures the three files apart
  and the panel's size cell shows the split on hover, so a WAL that dwarfs the sqlite file
  is visible without adding anything. It already dwarfs it: a two-cache run held a 4.1 MB
  WAL beside an 843 KB `beehive.db`, and a 4.8 MB one beside a cache file the same size.
  - **`cache_size` is the one with a memory cost.** Neither DSN sets it, so every connection takes
    modernc's 2 MB default out of an allocator that does not hand it back to the OS, and the count
    scales with the fleet: one writer plus `readerPoolSize` readers per cache file, over beehive's
    own pools. The reader pool is the cheap half to shrink — those connections stream rows.
  - **Measure first, and against a real fleet**: a cluster with CRDs, an event-storming
    namespace, and a cache that has been through a relist or two. The numbers that matter are
    file size versus rows held, and whether a vacuum sweep is visible as sync latency. None of
    these are guesses to apply blind.

- **Paged object watches.** Nothing pages anywhere: `clusterCachedDataObjectCollectionWatch` snapshots a kind's whole set and ships every `rawJSON` body over the IPC bridge. On a large kind that is the dominant cost of the dashboard — bigger than anything [the read split](adr/2026-08-29-object-read-split.md) addresses, which only shrinks the sidecar's half.
  - **The snapshot read bounds easily; the cursor reads do not.** Bound `ObjectsWithCursor` by a keyset range — `AND (namespace, name) > (?, ?) ORDER BY namespace, name LIMIT ?` — and the snapshot is a window. The changes read past the cursor name objects, not positions, so a window's watch has to re-derive the range when one lands in or next to it: an insert above the window, a rename that moves a row out of it, a delete that pulls the next row in are then just "what is in the range now vs. what was", never a shift to reason about. Two things are already in place: `objects_kind_ns_name(api_version, kind, namespace, name)` is exactly the keyset index for that order, and `kind_counts` gives the total for a scrollbar in O(1) with no scan.
  - **Settle the sort/filter contract first — it is the whole decision.** Paging trades *sort and filter by anything, client-side* for *sort and filter by what the store indexes*. `objects` carries `namespace`, `name`, `created_at`, `status_summary`, `ready_count`/`total_count`, `restart_count` and `host` as real columns, and `labels` is joinable for selector filters, so the common orders are servable. What is not: the kind-specific columns `src/components/dashboard/objects-table/columns/` derives from `rawJSON` client-side — sorting a paged Pods table by Restarts asks for an order the store cannot produce. Either accept that those columns do not sort, promote more of them into columns at write time (`projectObject` already extracts several), or do not page. A product call, not a storage one.
  - **The protocol wrinkle:** subscription variables are fixed for the life of a subscription, so moving the window means resubscribing — a debounced resubscribe per scroll settle, re-snapshotting a window's worth of rows. Cheap, but it makes the window a subscription variable (or a search param) rather than client state, and every move is a round trip.
  - **Not what the change log is for.** A log tail names the uid that changed, which is not a position in a sort order: inserting one object above the window shifts a row out of the bottom, and the entry mentions neither of the two objects whose page membership moved. Kubernetes offers `limit`/`continue` on list and nothing on watch for the same reason.

- **Namespaced watches.** Every objects and events watch streams a kind's whole set, so a
  dashboard page under a namespace scope filters client-side (a data gap of the new dashboard,
  [#881](https://github.com/kstackhq/kstack/issues/881)). The objects and events watches could
  take a `namespace` argument, as `clusterCachedDataKindCollectionWatch` does.

- **A health verdict per object and per kind.** The dashboard has no shared definition of a problem, so the sidebar draws no problem badges and no *Show only kinds with problems*, the kind pages' status tabs have nothing to count, and the Overview's *Needs attention* lists pods alone, judged in the webview (a data gap of the new dashboard, [#881](https://github.com/kstackhq/kstack/issues/881)). The client cannot fill the gap: the kinds watch carries counts alone, and judging objects in the webview would mean streaming every kind's objects to badge the sidebar.
  - **Decide the definition first — it is a product call.** Per kind, which state is a problem: a Pod failed or unknown, a container waiting on CrashLoopBackOff or ImagePullBackOff, or not ready; a Deployment, StatefulSet or DaemonSet below its desired ready replicas; a failed Job; a Node not Ready or under pressure; a PVC Pending or Lost. Whether restarts count, and which kinds have no verdict at all. Record it in an ADR.
  - **Shape:** the sidecar judges each object as it writes it, next to what `projectObject` already extracts (`status_summary`, `ready_count`/`total_count`, `restart_count`), and serves a verdict on `ClusterCachedDataObject` and a problem count on `ClusterCachedDataKind`, streamed like `count`.
  - **Then the webview:** a `ProblemBadge` on `NavItem` and `NavGroup` (a closed group sums its kinds), *Show only kinds with problems*, and Overview's badge, whose meaning the Overview page settles.

- **One measurement per cache for the stats state watch, not one per subscriber.** `cachesAPI.WatchStatsState` builds its whole loop inside `NewStream`'s pump (`services/cluster/caches.go:347`, `stream.go:180`), so every subscriber gets its own: three windows on one cache means three 5-second tickers, three file measurements, and three sets of row-count queries against the same file. The counts are the expensive half — the file measurement is three `os.Stat` calls, the counts are SQL. Nothing is wrong with the answers; the work is just done N times. **Shape:** one measurement stream per cache, multicast to its subscribers, the way the delta watches already fan out — the pump moves off the subscription and onto the cache, and a subscriber joins the running one and gets the current value on arrival (a state watch is current-on-subscribe, so a joiner must not wait for the next tick). `WatchHealthKeyedState` has the same per-subscriber shape over the records, so whatever carries the multicast should be able to serve both. **Not blocked on anything**, and independent of the cache size ceiling: the janitor's own measurement is a different caller with a different lifetime, and is not what this de-dupes.

- **A proactive refresh for the Kubetail Cloud connector.** The engine refreshes an access token only when `Sign` needs one (`services/connectors/internal/oauthconn/sign.go`), and nothing signs with the Kubetail session, so it is never refreshed: the app can show an account whose refresh token the issuer has already expired. What remains: a refresh before expiry, or a check at launch.
- **Run the ChatGPT connector's probe on a real account.** The connector is built on answers nobody has checked; each needs a ChatGPT plan and OpenAI's [devkit](https://github.com/openai/sign-in-with-chatgpt-devkit) for a token. (1) A whole tool round works with plain function tools in top-level `tools`, on a model the `openai` row lacks and on one it has; if not, the row wraps them in a namespace. (2) `reasoning.summary: auto` and `include: reasoning.encrypted_content` are taken; a refused one moves from the row's `Extra` to its `Omit`. (3) What `GET /v1/models` returns beyond `slug`, `display_name` and `visibility`, saved as the decoder's fixture. (4) Which listed ids the `openai` row also has. (5) The refresh token, in its `{"v","token","session"}` entry, fits Windows Credential Manager's 2,560 bytes. (6) A refresh straight after sign-in is allowed. (7) What a `client_id` the user deleted in ChatGPT's settings answers, at the token endpoint and on the authorize page.
- **Run the Kubetail Cloud connector's live checks against `oauth.kubetail.com`.** The connector is built on these assumptions: the discovery document's `issuer` is `https://oauth.kubetail.com` to the byte (no trailing slash), it names a `revocation_endpoint`, and every endpoint is on the issuer's origin; the ID token echoes the authorize request's `nonce`; and the token endpoint answers a revoked or expired refresh token `invalid_grant`. A wrong issuer, a missing endpoint or an unechoed nonce fails every sign-in: the issuer's default is in `sidecar/config.go` and the host's `OAUTH_ISSUER` (`src-tauri/src/services/sidecar/service.rs`), the session-ending code is the provider's `SessionEnders` (`services/connectors/kubetailcloud`). Then, by hand once: sign in from the avatar and from the tray, quit, relaunch offline, and the account shows with its name.
- **Run the AWS connector's live checks on a real account.** The connector follows the AWS CLI's `aws login` and the SDK's model of `signin`'s `CreateOAuth2Token`; these are assumed: AWS accepts the authorize request Kubetail builds (`scope=openid`, `code_challenge_method=SHA-256`, a loopback `redirect_uri` on `/oauth/callback` at a random port), a decline redirects with `access_denied`, the ID token's `sub` is the principal's ARN for an IAM user, a role session and root, and an expired or reused refresh token answers `AccessDeniedException` or `InvalidGrantException`. An answer that differs changes `services/connectors/aws`. Then, by hand once: Connect, quit, relaunch, refresh past fifteen minutes, and Disconnect. Once they pass, the AWS connector's row in `security-model.md` is **Enforced**.
- **Use the AWS connector's credentials.** `services/connectors/aws` is an `aws.CredentialsProvider` that nothing calls. Its consumers are Bedrock's row and the sandbox's AWS proxy (`services/awsauth`), each deciding whether the connector or a profile wins, with a security record of its own.
- **Run the OpenRouter connector's live checks on a real account.** The connector is built on these assumptions: a `callback_url` at a random `127.0.0.1` port is accepted, OpenRouter keeps the callback's query when it appends `code` (where `state` rides), a callback `error` is no decline the row can name, `key_label` labels the key, a user key cannot delete itself, and `GET /api/v1/key` answers a live key `200` and a deleted one `401`. An answer that differs changes `services/connectors/openrouter` and, for a decline, the webview's row. Then, by hand once with no `OPENROUTER_API_KEY`: Connect, run an agent turn on an OpenRouter model without restarting, and Disconnect. Once both pass, the connector-key row in `security-model.md` is **Enforced**.
- **Register Kubetail's GitHub OAuth App and run the GitHub connector's live checks.** The connector needs an OAuth App with the device flow on; its client id becomes `--github-client-id`'s default in `sidecar/config.go` (or the host passes it), and until then `gitHubConnect` is refused. It is built on GitHub's documented device flow: JSON answers under `Accept: application/json`, `200` with `error` while pending, `slow_down` naming its `interval`, and `/user` answering a revoked token `401`. Then, by hand once: Connect, enter the code, run a sandboxed `gh repo view` and `git clone` of a private repository with no `gh` login, revoke Kubetail on GitHub and see the next request drop the token (`KeyRejected`). Once both pass, the GitHub connector's row in `security-model.md` is **Enforced**.
- **Register Kubetail's Google OAuth client and run the Google connector's live checks.** The connector needs an OAuth client for installed apps with `cloud-platform`, a sensitive scope Google verifies the app for; its id and secret become `--google-client-id` and `--google-client-secret`'s defaults in `sidecar/config.go` (or the host passes them), and until then `googleConnect` is refused. Unchecked against Google: (1) the token endpoint takes `client_secret` in the form, and the authorize and token endpoints accept a request with no `resource`; (2) `nonce`, `access_type=offline` and `prompt=consent` on the authorize request return a refresh token and an ID token carrying the nonce; (3) the token response's `scope` names `https://www.googleapis.com/auth/cloud-platform`, and one with that box unticked does not; (4) a revoked grant refreshes `invalid_grant`; (5) Vertex AI's OpenAI-compatible endpoint, global and regional, serves each `google/<model>` id the row lists, with its efforts, under the bearer; (6) the refresh token fits Windows Credential Manager. Once all pass, the Google connector's row in `security-model.md` is **Enforced**.
- **Share the keyring entry between the OpenRouter and GitHub connectors.** Each `keyring.go` (`services/connectors/openrouter`, `services/connectors/github`) holds its own copy of the versioned entry's rules: a newer `v` is left in place, an entry that does not parse is deleted, an unreadable keyring writes nothing. Move the load, save, delete and check into one package under `services/connectors/internal`, typed by the entry's payload, in a PR of its own: it moves the tests `docs/persisted-data.md` names for both formats. `Disconnect` and the sign-in state stay per connector until a third one shares them.
- **A Kubetail sign-in that fails after the browser opens is not retried.** The failure is logged and on the connector's state (`Failed`), but `AuthState` draws it as signed out and names no failure, so the user only sees that the sign-in did not take and must start it again.
- **Check RBAC permissions?** The `ClusterPermissions`/`ResourceRule`/`NonResourceRule` types and schema exist, but the `Permissions` resolver is a stub that returns `not implemented: permissions`. Implement it via a `SelfSubjectRulesReview`. Distinct from the `SelfSubjectReview` *authentication* probe behind `ClusterPrincipal.username`, which is implemented.

## Sidecar (Go)

- **Run the keyring's manual checks.** `oskeyring`'s gate and the background restore are tested
  over fakes alone. By hand, with a throwaway program over `oskeyring.New`, recording what differs
  from the code in the pull request that corrects it:
  - Linux, the Secret Service stopped, then its unit masked so auto-start fails: the probe's error,
    its Go type and its D-Bus name, which `unavailable_linux.go` must match as `ErrUnavailable`.
  - Linux, no session bus: the probe fails at once and is not retried.
  - Linux, the login keyring locked: the probe raises one prompt; unlocked, it answers
    `ErrNoEntry`; dismissed, an error that is not `ErrUnavailable`.
  - macOS, the login keychain locked (`security lock-keychain`): whether the probe, a lookup of an
    item that is not there, raises the unlock dialog, and whether the reads that follow share one
    dialog. **If they raise several, the one-prompt promise does not hold on macOS:** serialize the
    restores' reads behind the gate, and say so in the ADR that follows
    [a launch restores keyring credentials in the background](adr/2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md).
  - The app, on Linux with the login keyring locked, for a keyed provider no connector signs with
    a saved key and its environment key both: the window opens with the prompt up and every
    connector row and watched provider row *checking*; a turn to that provider waits; unlocked, the
    rows settle, the provider on *Using a saved key*, and the turn runs. Dismissed instead: one
    prompt, every connector signed out, the provider on its environment key, the turn under it.
- **Read the keyring again after a restore found it unreadable.** A dismissed prompt or an
  unreadable keyring fails `oskeyring`'s gate for the process: every service settles signed out
  until a later launch. Wanted: a way to ask again (a
  *Try again* in Settings, or a retry on the next sign-in), which gives the gate a new opening.

- **Kubetail has no app-level data migration.** Each format migrates itself when it opens, and a
  SQL migration changes tables only, so nothing can move, rename or remove a path under `<data>` or
  `<cache>`, or rewrite a JSON column SQL cannot express. Until it exists the layout only grows
  ([`persisted-data.md`](persisted-data.md), *What cannot change yet*). **Shape:** an ordered list
  of Go steps run at start, each recorded once done. **What to settle first:** where the position
  lives. In `app.db`'s `schema_migrations`, numbered with the SQL files: one ordered list,
  committed with the SQL half, and every build back to `v0.0.1` already refuses a higher version.
  But the host cannot read it, a step cannot run before `app.db` opens, and if `app.db` ever goes
  away the version goes with it, so an older build would make a fresh, empty `app.db` instead of
  refusing; the release that removes it would have to leave a file holding the version. In a
  marker file of its own under `<data>`: the host can read it and it outlives `app.db`, but builds
  before it never read it, so a downgrade to one across a step is not refused, and it can disagree
  with `app.db` if a crash falls between a SQL migration and the marker's write. Either way a step
  runs outside a transaction and must be safe to run again after a crash (a move checks that the
  old path exists and the new one does not), and each step needs a test that runs it twice.
  **Trigger:** the first change that needs a path moved, renamed or removed, or a JSON rewrite SQL
  cannot express.

- **Offer kubetail when a log stream stops at a limit.** A stream past the normal limits stops
  with `kubelet.FailLimit` and is named in a `stream-skipped` notice (→ [ADR: backward log reads assume normal
  logs](adr/2026-10-18-backward-reads-assume-normal-logs.md)). Kubetail, installed in the
  cluster, reads rotated files and greps on the node, so it can serve what the kubelet cannot.
  **Shape:** a second `Backend` after the kubelet, and an offer in the viewer next to the reason,
  which needs a limit told from a failure first.

- **A long log line is cut at write time, and nothing says so.** `logline.KeepColor` (`logline/clean.go`) keeps
  16 KiB of a message and appends an ellipsis; the file's `lines` row holds the cut text and
  nothing else, so no reader can tell a cut line from one that ends in `…`. The cap stays: a line
  is cluster text, and one line of many megabytes must not grow a buffer, a row or a page. Three
  pieces, in order:
  - **The kubelet fetcher should stop reading a long line, not skip past it.** `readLine`
    (`kubelet/read.go`) keeps 64 KiB of a line and reads the rest of it off the wire to find its end,
    so an attacker's line still costs its whole length in download. Stop at the cap, close the
    read, and step forward to the next timestamp instead.
  - **Record that a line was cut, as a boolean.** A `lines` column the writer sets when the cap
    bit, the only fact the fetcher can know without reading the line whole. The readers (`Fetch`,
    `Follow`, both tools, the viewer) surface it; the ellipsis leaves the stored text, since what
    to draw is the reader's call. `docs/security-model.md` pins the cut with an ellipsis
    (`TestStripControlCutsALongLine`); move that row in the same change. Until then `Fetch` and
    `Follow` take the file's line as the whole message, and `TruncatedBytes` counts only the
    request's cut.
  - **A safe way to read the whole line.** A bounded read of one line from the kubelet on request,
    streamed to the caller in parts under a byte budget, never held whole and never written to
    the file.

- **Bound a log scope's size, then cap the page cursor.** A scope's sources and filter values have
  no size limit, and `MaxStreams` bounds the streams the mirror resolves, not the cached streams of
  gone pods read beside them. So a page cursor, which carries the scope and an entry per container
  passed, has no bound either, and `decodePageCursor` caps only its inflation. **Shape:** a
  serialized scope budget and a total container limit, both checked before a page is served, so
  the decoder can refuse an oversized token without ever refusing one the service wrote.

- **Give a grep its context lines.** `Fetch` applies `Grep` to the range alone and leaves the
  padding unfiltered (rule 11), so nothing shows the lines around each match. **Shape:** a
  `Context{Before, After int}` beside `Grep`, refused without one; lines of the match's own
  stream, attached to the match record rather than merged into `Records`, so `PageSize` still
  counts matches and the cursors do not change; a line that is itself a match is never another
  match's context; clipped to the window, capped, and counted against `MaxPageBytes`. The walk
  already passes every line of a stream, so before-context is the last N lines per stream and
  after-context is N more, with a short read past the cut for a page's last matches. Ship it on
  `Fetch` first: on a follow, after-context means holding a match until N more lines arrive.

- **A chat's directory layout is named in two packages.** agent/chat owns `<data>/chats/<id>`, but
  `tools` names two of its entries (`workspace/` in `WorkspacePath`, `results/` in `SaveTo`) while
  agent/chat names the third (`tasks/`), and agent/chat calls `tools.WorkspacePath` to learn its own
  workspace's path. Fix when the layout next changes: let the owner name every entry, keeping the
  link-safe opening in `tools`.

- **A report per path the sandbox refused a probed executable.** The executable probe draws a failed executable's
  own error and nothing more; which path the sandbox refused is the user's to read off it. Both
  platforms can say: Seatbelt logs every denial, and a profile can tag each deny with a message
  read back off `log stream`, while on Linux the forwarder sits above the seccomp filter and
  could trace the run's path syscalls. A line per denied path with a grant beside it is the shape,
  whether as the default or as an option the user turns on.

- **Explore a janitor for the Bash tool's caches.** Bash sweeps `<cache>/kubectl` only at start,
  removing the cache of a cluster that is gone, so a deleted cluster's cache lasts until the next
  start, and a live cluster's grows unbounded: each new server identity adds a directory, and
  kubectl never trims its HTTP cache. A janitor running while Kubetail does could remove a cluster's
  cache once the cluster goes, a server identity's once it is superseded, and any cache unused for
  a while, as `kubestore`'s janitor bounds the mirror.

- **Let a background command's cluster writes be asked (design first).** Today bash's `writesFor`
  refuses every write a background command sends, deciding for the asker that nobody can be
  asked. Move that decision to the asker: bash hands every sandboxed call its asker, refuses only
  when there is none, and the asker answers a write it cannot put to anyone with an error the
  proxy maps to a refusal. What is left is how to get the request in front of the user in time.
  - **Not a one-line change.** agent/chat's asker is bound to the run: it files a write under the
    journal's `openTool`, the call running now, and ends its wait with the turn. A write a
    background command sends later would land on an unrelated call, or be abandoned at once. The
    asker has to be per call, asking as it does today while its call is open and through a late
    path once the call has returned.
  - **The late path needs** a bound on the wait, since a background command ignores `timeout`
    (the agents' 30 minutes is the model); a *Waiting on you* that does not hang off
    `agent_runs.status`, since the turn may be over — pending approvals could drive it
    everywhere; and a home for the request, the call row its `background_tasks` row points to.
  - **The UI questions:** where a request is drawn once its call closed long ago; what context
    it carries, the command, when it started and the model's description, since the explanation
    is far up the transcript; whether a deadline needs a notification outside the window; and how
    it reads apart from a foreground request. The request is the whole gate, so this needs its
    own ADR and security record.

- **Let a user flag a context as production (when users ask).** Every context starts at the
  default mode, and a user marks a production one by setting its mode to read-only or adding a
  `Deny` rule. A flag could carry its own defaults: read-only, and namespace deletion refused in
  every mode. Guessing it from the context's name (`*prod*`) was weighed and left out, since it
  catches `nonprod` and misses a production cluster named otherwise → [ADR: permissions are
  access levels, a boundary, modes and rules](adr/2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md).
  **Trigger:** a user asks for it.

- **Ask for a sandboxed command's cluster reads under a locked-down mode (design first).** Today
  the proxy forwards every read unasked. A mode that asks for reads too could record each one as
  an approvals row beside the writes, in the same request shape with no body, so the table needs
  no change. What is left:
  - **Volume.** `kubectl get pods` sends discovery requests (`/api`, `/apis`, sometimes OpenAPI)
    before its list, so one command is ten requests or more. Asking for each is unusable: the mode
    has to let discovery through, or ask once per command or per resource.
  - **Who decided.** If a rule decides most reads, a row has to say whether the user or the rule
    approved it, a column added with the mode.
  - **Watches.** One approval covers a stream that stays open; what it approves has to be said.
  - **The proxy.** Reads skip the asker and writes wait one at a time behind the write lock; asked
    reads need their own answer to both.

- **Give the Bash tool a fallback toolbox.** A command runs the user's own tools, off the
  snapshot's `PATH`, and a machine without `kubectl` leaves the agent unable to read a cluster. Ship
  a small read-only set the agent's own work needs — `kubectl`, perhaps `helm` and `jq` — at pinned
  versions with checksums, `kubectl`'s following the cluster's server version, and put it on
  `PATH` after the user's, so a tool the user has always wins. Kubetail downloads and updates it; no
  command can write it, so no chat can plant a binary another runs. Any other tool stays the
  user's: the model asks, and an approved command installs it through their package manager. Not a
  shared writable `bin/`, which would let one chat's command change what every later command runs.

- **Give what runs inside the sandbox a binary of its own (low; sandbox owner).** A sandboxed run with a cluster
  re-runs the whole sidecar binary as `kubetail-sidecar sandbox-init`. Before `main` reads the
  subcommand, Go runs the `init` of every package the sidecar imports, 365 of them, inside the
  sandbox, on every call. None touches a file or the network today. One added later that does would
  break every sandboxed call, or log a Seatbelt denial on macOS, and nothing in the sidecar says so.
  It also costs time and memory. Measured on Linux with a release build, a start takes 6.9 ms and
  holds 7.4 MB of anonymous memory. A Go binary with only the forwarder's code takes 1.0 ms and
  1.7 MB. Every call starts the binary twice, as `sandbox-init` and `sandbox-shell`.
  **Shape:** a Go program under `sidecar/cmd/` that shares no code with the sidecar. A test of its
  imports refuses any package of the sidecar's module. `sandbox-shell` goes in it too, since it
  also runs inside the sandbox.
  **The interface is a contract, not code:**
  - `[--socket <path> --port <n>] -- <argv…>`, the two flags together or neither;
  - the port listened on before the child starts;
  - each connection relayed byte for byte, half-closing each side as the other ends;
  - `SIGTERM`, `SIGINT` and `SIGHUP` caught, never ignored;
  - the child's status as the exit code, 128 plus a signal that killed it;
  - 125 and one `sandbox-init:` line on stderr for its own failures;
  - as a PID namespace's first process, every orphan reaped and its memory non-dumpable;
  - a core size of zero for the child, and one P, so its threads stay within `forwarderTasks`.

  The sidecar keeps `Sandbox.argv` and its own `ExitCode`. The forwarder's tests already drive a
  process and read only its exit, output and port, so they become black-box tests against the
  built binary. `TestMain` builds it with `go build`, so `go test ./...` needs nothing else.
  **Go, not Rust:**
  - Rust would save about another 0.5 ms and 1 MB.
  - It would tie the sidecar's tests to a Cargo build, which some dev machines lack.
  - Go covers what `sandbox-shell` needs: `syscall.AllThreadsSyscall` installs a seccomp filter
    with `CGO_ENABLED=0`.

  Revisit Rust if a measurement shows the Go binary's start or memory mattering, or if
  `sandbox-shell` turns out awkward in Go. The black-box tests make that swap safe.
  **What it costs:**
  - a second executable for `scripts/build-sidecar.go` to build per target;
  - the host shipping it beside the sidecar;
  - macOS signing and notarizing it;
  - `Probe` finding it next to `os.Executable()`, and reporting no sandbox without it.

  **Trigger:** met, since `sandbox-shell` doubles the starts on both platforms and the `init`s
  run confined under bubblewrap.

- **Reorganize the model, tool and run-loop code into `llm` → `tools` → `agent` → `agent/chat`.** `llm` speaks to models, `tools` is what the sidecar can do, `agent` runs a loop over the two, and `agent/chat` owns chats, rows, approvals and the live view, with Go enforcing the one import direction; the spec sequence is written when the work starts.

- **Give each chat a scratch directory, and run bash there.** Two steps. First, each chat gets
  `<dataDir>/workdirs/<chatID>`, owned by `agent/chat`: made owner-only just before a tool uses it,
  removed with the chat — never following a link a command may have put in its place — and swept
  at start for chats that are gone. Second, it replaces the user's home as bash's default
  `workdir`; its prompt then says a file written stays for the rest of the chat, and stops
  steering throwaway files to `mktemp`. The directory lives as long as the chat
  because the chat's history does: a file the model saw written must still be there on the next
  turn. It is the data directory's, not the system temp's, which the OS empties on its own
  schedule. A later file tool is confined to it. A place, not a boundary: an approved command
  still reaches what the user can, and the change carries a dated note on the [bash security
  record](security/2026-09-18-bash-tool.md).

- **Run bash under the user's umask.** `main` sets the process umask to `077`, and every command
  bash starts inherits it, so a file an approved command makes is owner-only where the user's
  own shell would make it `0644`. `app.Config.UserUmask` carries the umask `main` replaced, which
  Write already applies ([security record](security/2026-09-24-write-tool.md)); bash's wrapper can
  set it before the command runs.

- **Let `deepseek-reasoner` take tools.** It is the one Chat Completions entry held back from
  tools: its documentation has said both that a round's `reasoning_content` must come back on
  the assistant message that carried the calls and that sending it back is refused, and this
  wire's walk sends no thinking either way. The check is a listing and a follow-up turn on a dev
  build with a key; if the reasoning must return, the shape is a payload on the wire's thinking
  block for a row that says so, and an extra field on the assistant message the walk builds.

- **Explore a Relay-style mutation shape.** Every mutation takes flat arguments today, and
  `chatSend` has eight of them; the Relay convention is one `input` object per mutation
  (`chatSend(input: ChatSendInput!)`), with a `chat.SendRequest` behind it so `Send`'s
  positional arguments become one value. Decide it schema-wide — one mutation on the convention
  and the rest off it is worse than either — and whether the matching `…Payload` types come with
  it. Nothing is waiting on it.


- **`safe.Redact` runs at ~2.4 MB/s (low; sidecar owner).** Twelve passes, and the eight
  anchored on `\b`, `^` or a case-insensitive alternation have no literal prefix, so the regexp
  engine steps at every byte — `headerRE` and `netrcRE` alone are ~45% of the whole; the rules
  with a literal prefix (`-----BEGIN`, `--`, `eyJ`) run at GB/s. Measured: ~25ms per 64 KiB
  bash capture, ~40µs per log line, linear, allocation ~2 MB per capture. Bash and `Read` now
  redact up to 8 MiB a call, about 3.5s, which bash's `callMargin` and snapshot allowance and
  `Read`'s 30s bound absorb, and which `Read` repeats on every range of a file. Fix it when that
  is felt. The fix then is a prescan
  gate per slow rule (skip `netrcRE` unless a case-folded `password` appears, `headerRE` unless
  a header name does): cheap, but a second spelling of each rule's anchor to keep in sync, which
  is why it is not done now.

- **Nothing measures the sidecar's own memory.** No package imports `net/http/pprof`, so every
  question about footprint is answered by inference. A release build must not carry the handlers,
  but `debug.go`'s build tag already gates a dev build and the socket is pid-authenticated and
  owner-only, so registering them on the h2c mux under that tag ships nothing to a user. **What
  prompted it:** a run mirroring two caches — ~5 MB of SQLite between them — held 76 MB resident,
  68 MB of that dirty anonymous memory, beside a 20 MB clean `__TEXT`. A lot of headroom for the
  data held, and nobody can currently say which subsystem owns it.

- **No soft memory limit.** Nothing calls `debug.SetMemoryLimit`, so the heap settles wherever
  `GOGC` puts it above the largest burst the process has seen, and the scavenger unwinds that at its
  own pace. A desktop sidecar has a ceiling a server would not: a limit plus a lower `GOGC` trades
  CPU during a sync for a footprint that stops ratcheting. **Not blind** — the number to set is
  whatever the profile above reports as steady-state live heap, and picking one before that is
  picking at random.

- **The byte counts on `ClusterCacheStats` are `Int!`, which the GraphQL spec defines as signed 32-bit.** `bytes`, `dbBytes`, `walBytes`, `shmBytes` and `sizeLimitBytes` are all `int64` in Go, and the default size limit (2 GiB) is already past the 32-bit maximum. Nothing breaks today: gqlgen's `MarshalInt64` writes the digits with no range check, and the webview maps `Int` to a TypeScript `number`, which holds the value exactly. A spec-strict client would refuse it. **Trigger:** the first client that is not the webview. **Fix:** a custom `Int64` scalar in `schema.graphqls`, bound in `gqlgen.yml`, mapped to `number` in the frontend codegen config, and applied to all five fields in one change — not per field, or the type says two things about one quantity.

- **`agent/chat`'s watches re-read a whole transcript to diff it; a write log is not the answer, a read
  split might be.** Every `messages/<chatID>` ping re-reads the chat's rows and diffs them by id
  (`readMessages`, `foldMessagePing` in `sidecar/internal/agent/chat/service.go`), and the rows carry
  `content` — the verbatim content blocks — so `fold` compares whole message bodies to learn that
  one row settled. **Nothing here is hot:** that pull runs a handful of times per turn (the send,
  the settle, a rename, a delete), while the per-chunk path rides `stream/<chatID>` and reads
  nothing at all, rebuilding the overlaid message from the turn's memory.
  - **An `object_writes`-style log is the wrong trade at this size.** Of what a log buys the
    cached-data watches (→ [ADR: write positions](adr/2026-08-30-write-positions-and-the-deletes-log.md)), none lands here: the collection is one transcript; the
    coalesced net is what a chat wants, not every intermediate streaming state; and a resumable
    cursor has the same missing consumer, since `useFoldedSubscription` drops the previous
    generation's accumulator on reconnect. It also costs more here than there — an authoritative
    log appends in the write's own transaction, which puts an append on the checkpoint path (the
    one write the service is allowed to fail and skip), and a delete entry carries a final row
    image, so the log doubles the write volume of the largest column in the schema.
  - **The cheaper answer, if a cost ever shows up:** the read split the objects watch already took
    (→ [ADR](adr/2026-08-29-object-read-split.md)). Read identity for the diff — id, seq, status,
    `completed_at`, the token counts — and fetch `content` only for the rows that become `Added` or
    `Modified` frames. No table, no migration, no retention sweep, no horizon.
  - **Trigger:** a transcript long enough, or thinking blocks large enough, that a rows ping is
    visible in a profile. Unmeasured today, and small conversations will not produce it.

- **Investigate scoping `chatCollectionWatch` by mode and cluster server-side.** The watch is unscoped and the webview does both filters itself (`chatModeOf` and `clusterID` in `src/lib/chats.tsx`), so every window holds every chat of every cluster in both modes to draw one list. Scoping it to `(clusterID, mode)` would ship each window only its own. **What it costs.** `OpenChat` tells a deleted chat from another cluster's by looking in the list, so a scoped list leaves it nothing to look in — it would need a `chat(id)` read of its own, and the out-of-scope notice would then be driven by that read rather than by the fold. urql keys an operation on its variables, so each mode and each cluster switch opens its own connection and cold-lists, where today one watch serves the window. And the SQL needs an index the table does not have: `chat_by_recency` is `(updated_at DESC)`, and a scoped newest-first read wants `(cluster_id, mode, updated_at DESC)` — an `app.db` migration that creates it. **Trigger:** a chat count at which sending every chat to every window is visible in a profile, or a second consumer of the list that cannot filter client-side. Unmeasured today.

- **Explore comparing cluster cards by hash rather than by text.** `questionContent`
  (`sidecar/internal/agent/chat/service.go`) decides whether a send carries a card by comparing the
  freshly rendered card, byte for byte, against the newest context block in the chat's record
  (`newestCard`, a `json_extract` over the chat's user rows). Both sides are up to 4 KiB, and the
  read walks the rows since the last card. A hash of the card beside the message — a column on
  `messages`, or the hash inside the context block itself — would make the compare a fixed
  few bytes and the read an indexed seek. **What to settle first.** Whether it is worth having:
  the compare runs once per send, over one chat's rows, and is unmeasured; a hash is a second
  thing the record has to keep in step with the block. Where it lives if so: a column is the
  boring shape, an `app.db` migration that adds it and leaves it NULL on the rows before it, so
  the compare falls back to the text there; a field in the block would reach
  the wire and the model. And that equal text stays the definition — a hash is a shortcut for
  it, so a collision test belongs beside `TestSendCarriesTheCardOnlyWhenItChanged`. **Trigger:** a
  send whose compare is visible in a profile, or a second kind of context that makes the block
  large enough to matter.

- **A failed answer is never retried.** `agent/chat` settles a turn that failed as a `failed` row
  carrying the text that arrived and the reason (`answer`/`settled` in
  `sidecar/internal/agent/chat/service.go`); only the *write* of that row retries. The policy is
  deliberate — a model call costs money and the user did not ask twice — but it does not
  distinguish a refusal from a 429 or a connection dropped three tokens in.
  - **The re-typing is answered**: the transcript's last row, when it is a failed answer, draws
    **Ask again** — a fresh send under a new `requestID` carrying the question it followed and what
    that row ran on (`askAgain` in `src/lib/chat-outbox.tsx`). That is the user asking twice, which
    is what the rule reserves for them. What is left below is the sidecar deciding for itself.
  - **Fix:** classify the provider's error, and retry inside the turn on the transient half alone,
    on a bounded ladder the tests can shrink (a parameter, like `checkpointEvery`). The provider
    knows what its transport said, so the classification belongs behind `Provider`, not in the
    service reading error strings.
  - **What makes it more than a loop:** a retry restarts the answer, so the text already streamed
    has to be dropped — the overlay rewinds under every open watch, and a checkpoint may already
    have written it. Decide whether a watcher sees the rewind or a retry only ever happens before
    the first chunk. `Cancel` and `stop` must end the wait, not just the call.
  - **Weigh:** the honest alternative is to leave the retry with the user and make the failure
    legible instead — a `failed` row the UI offers to resend, which is one client change and no new
    state in the service. Build the ladder only if the transient failures turn out to be common
    enough that a person clicking resend is the worse answer.

- **Return an error from `marshalBlocks` instead of panicking.** `agent/chat/record.go`'s
  `marshalBlocks` panics when a block's `Input` or `Payload` is not valid JSON. No path reaches it
  today: `ToolUseBlock` and `ServerUseBlock` refuse bad input, the wires check every payload
  (`Block.replayable`), and stored rows are decoded by `unmarshalBlocks`. But a `Block` built by
  hand would reach it, and from `Settled` the panic is recovered only after `settle` has set
  `t.settled`, so `runTurn` skips its own settle and the answer reads streaming until the next
  start marks it stranded. Returned instead, `settle` passes the error on (the documented stranded
  path), `writeTurnRows` fails the send, and `Progress` logs it and keeps the last content.

- **A full chat could continue.** Every turn replays the whole transcript (`buildRequest` in
  `sidecar/internal/agent/chat/service.go`), so a chat grows until the model can no longer read it.
  `roomFor` (`sidecar/internal/agent/chat/context.go`) makes the end honest — a send past the
  model's window is refused with `KUBETAIL_CHAT_CONTEXT_FULL` and the draft stays —
  but a refused chat is over on that model, and a long investigation is exactly the chat a user
  wants to keep. What would let it go on, each a different trade:
  - **Clear old tool results.** Keep every `tool_use` and replace the `tool_result` text of turns
    before the last few with a stub. Provider-neutral, done in `buildRequest`, and the record is
    untouched — only what is sent changes. Loses the model's memory of *what it saw*, keeps its
    memory of *what it did*. Anthropic's `clear_tool_uses_20250919` does the same server-side.
  - **Server-side compaction.** Anthropic's `compact-2026-01-12` (beta, Anthropic only) returns a
    compaction block that must be stored and replayed in place of the history it summarises.
    That block would have to live in the record and `buildRequest` would have to honour it, which
    cuts against [the answer is a record](adr/2026-09-10-the-answer-is-a-record.md): the
    transcript would no longer be what was sent.
  - **A summary turn.** The sidecar asks the model for a summary and starts a new chat seeded
    with it. No provider feature, the record stays honest (two chats, the second's first message
    says where it came from), and the user sees the seam. The cheapest to build and the easiest to
    explain.
  - **Weigh:** the first is a change to `buildRequest` alone; the third is a product decision
    about what a chat is; the second is the only one that keeps one chat *and* the model's memory,
    at the price of a record that is no longer the request. Whichever is chosen is an ADR.
  - **Trigger:** the first user who hits the refusal on a chat they wanted to continue.

- **Record the system prompt each turn was sent.** A stored turn says what the model answered
  and what it cost, but not what it was told: `systemPromptFor` (`sidecar/internal/agent/chat/service.go`)
  picks `toolsPrompt` or `noToolsPrompt` per turn by encoder, and `prompts/system.md` is embedded
  from source, so reading a chat back later means guessing which prompt, from which build, ran it.
  - **Store the text, not a version number.** A version cannot say which of the two prompts a
    turn got, nothing stamps one into the sidecar today, and a build between releases changes the
    prompt under an unchanged number; a version is a pointer to a checkout where the record should
    describe itself, as `content` stores the blocks that went over the wire rather than what
    produced them. Content-address it so the 5 KB is stored once per distinct prompt however many
    turns sent it: a `chat_system_prompt (hash PRIMARY KEY, text)` table and a
    `system_prompt_hash` on `agent_runs`, which a user message has none of. The prompt is known at
    `Send` (the encoder is fixed at `reserve`), so `INSERT OR IGNORE` the row in the same
    transaction as the assistant placeholder and carry the hash on the `turn`.
  - **Per message, not per chat**: the encoder can differ turn to turn, and an upgrade mid-chat
    changes the prompt for the next turn alone.
  - **The tool definitions are the other half** of what the model was told and change on their
    own (`s.clusterTools.Definitions()`); hash the marshalled list the same way, or note here that they
    are left derivable from the prompt's era.
  - **Keep it off the watch.** It is operator text, not cluster data, but 5 KB on every frame is
    the wrong place; resolve it lazily or through its own query.
  - A `kubetail_version` column beside the hash is a cheap addition once a version is stamped into
    the build — it names the loop code that ran when two builds share a prompt — but not a
    substitute for the text.

- **The transcript draws a tool round as a blank line.** `tool_calls` holds a row per call
  under the model call that asked (`llm_call_id`) — status, arguments, full result or error,
  start and finish — and `llm_calls` a row per round, so drawing a turn call by call is reading
  a run's rows in id order; the `tool_use`/`tool_result` blocks in `content` are the wire's copy
  of the same. Nothing on the wire carries the rows yet: no GraphQL field reads them.

- **A call that breaks off has no usage.** `llm_calls` closes an interrupted model call with
  its error and finish time — its duration counts — but its four token columns stay NULL:
  `Reply.Usage` is zero on a failed reply (`sidecar/internal/llm/llm.go`), and the store keeps
  unknown apart from zero. Cancel while an answer streams is the common case: the provider read
  every input token and streamed some output, and the counts on that row are unknown.
  - **Fix:** record what the provider said before the break. Anthropic's `message_start`
    carries `input_tokens` and the deltas received count the output; OpenAI's stream is the same
    shape. Let the encoder hand a partial `Usage` to `Fail` with `Reported` set, and the store's
    conversion takes it as it does a complete report. One adapter change per dialect.
  - Until it lands, the counts on a `Cancelled` or `Failed` row are the finished calls' alone,
    and the resolver descriptions should say so.

- **Hoist `Condition`/`Event`/`Schedule`/`ObjectRef` when a second consumer appears.** All four are kind-agnostic on the wire (unprefixed, per the schema's naming rule) but live in `internal/services/cluster` (`shared.go`) because the cluster surface is their only consumer. **Trigger:** the first non-cluster kind or subsystem that needs conditions, events, schedules, or owner refs — at that point move all four into a shared leaf package (e.g. `internal/lib/apimeta`), leaving `services/cluster` its `ConditionType` constants (`Connected`/`Identified`/`Synced`). `ObjectRef` takes `toOwnerRef` with it. Hoisting earlier would be a one-importer abstraction.

- **`appdb.Close` during the janitor's first sweep can leave `app.db-wal` behind.** The sweep starts the moment `Open` returns; a `Close` that cancels it mid-statement leaves the WAL beside the file (about 1 in 200 in a loop of `Open` then `Close` after a short delay). A failed `app.New` that closes the file right after opening it hits this, and a test asserting the WAL's absence flakes. Wanted: `Close` leaves no WAL after joining the janitor, e.g. an uncancelled checkpoint before the pools close.

- **Give `app` a runtime struct for what `main` measured at launch.** `app.Config` mixes the
  settings `main` read from flags and the environment with facts it measured before `run`:
  `HostPID`, `UserUmask`, and the login shell's answer, split across `ShellPath` and `ShellFault`.
  Move the facts into a struct of their own. Carry the shell's answer as one field, a path or the
  reason there is none, whose zero value means `main` did not run the shell (tests, Windows).
  - **One way into the PATH's state.** The launch's fault enters `settings.Service` through
    `NewService`, while a refresh's goes through `RefreshPath`. Give the service one method that
    records a resolution: the fault when it failed, else a sync of its path. The launch's PATH
    sync part and `RefreshPath` both call it, and `NewService` loses its `fault` argument.

- **Rename `agent/chat` to the agent-run service it is.** The package owns every agent run: a chat's
  turns, the subagents, the background tasks, the permissions askers, the folder grants the
  executable probe reads, and the monitor run, which has no chat at all. "chat" names the one run it started with. A name such as
  `agentsvc` or `runsvc`; the chats, their messages and their watches stay inside it.
  - **Scope.** `git mv` the package, fix its importers and the gqlgen binding, regenerate, and
    update both `CLAUDE.md`s and the security records' code references. Leave the
    ADRs. No table or wire name changes.

- **Build the monitoring agent over `chat.RunMonitor`.** The run, its session, its record, its
  folder and its teardown exist; nothing calls them but their tests.
  - **Schedule and brief.** When a run starts, what it is told, what it keeps between runs (the
    last run's `task` and `result` are there to read), and how its model is picked.
  - **Settings.** The per-target switch (`monitor_targets.enabled`, served as
    `clusterMonitoringEnabledSet` until step 2), which gates detection and is on by default,
    drawn only once something runs behind it, and the note's separate switch for sharing folder
    grants with the monitor.
  - **Proposals.** A card for what the monitor proposes, with two fixes from 6B's review. The
    unstarted chat's draft is keyed `new:<mode>`, not per cluster, and outlives a cluster switch,
    so a proposal joined onto it must carry its cluster and Send must hold while the window is on
    another. A proposal is text the monitor wrote from cluster data: drawn through `VisibleText`,
    and a question only as the user's own draft, never sent on a press.
  - **Findings.** A view of what the runs found, its reports drawn as text.

- **Rethink how settings sync (design first).** Settings live in two places today, and neither
  syncs: `host.json` (the host's, such as the color scheme) and `app.db`'s rows (the sidecar's).
  Decide which settings follow a user to another machine; whether a security setting may ever
  sync, and if so only one that narrows, since a value from the cloud must never widen what a
  sandbox may do; and how the rows are scoped, since the `PATH` setting and the executables
  describe one machine. The sync design needs an ADR.
  **Trigger:** the first setting that has to sync.

- **Explore refactoring the login shell code.** Its three runs are spread over two packages and
  six platform files: `app` runs the launch resolution (`launchshell_*.go`) and builds Refresh
  PATH's resolver (`shellpath_*.go`), and `bash` takes the snapshot (`snapshot_unix.go`), all
  through `loginshell`. The launch and the refresh each build the same `loginshell.In` by hand,
  the scratch folder comes from `bash.TempDir`, and `setShellEnv` sits in `app`. Look for a
  shape where `loginshell` owns the runs and their policies and its callers ask for an answer:
  one place that knows the resolution shuts the denied-always list and the snapshot does not,
  and fewer platform files.

- **Explore moving the sandbox into a library of its own.** `internal/run/sandbox` imports nothing
  else of Kubetail's: it confines a command under bubblewrap or Seatbelt from a `Policy`, with the
  lists, the probe and the forwarder. A Go module of its own would give it a public API, its own
  tests and releases, and a use outside Kubetail. What ties it to Kubetail today: the
  `AlwaysPolicy.Kubetail` field, the forwarder and init running as the sidecar's own binary
  (`sandbox.Main`), and the bwrap Kubetail ships beside it on Linux. Decide whether those become
  options, and whether the API is settled enough to publish before steps 4C and 4D change it.

## Host (Tauri/Rust)

- **The tray's account section draws a restoring account as signed out.** The section is hidden
  (`SHOW_ACCOUNT_SECTION` in `src-tauri/src/tray/mod.rs`). `account_snapshot_from`
  (`tray/account_menu.rs`) reads `authenticated` alone, so until the launch has read the keyring
  the section would offer *Sign in / Sign up*, and the sidecar refuses a sign-in while
  `restoring`. **Fix, before showing the section:** carry `AuthState.restoring` into
  `AccountSnapshot`, give `AccountMenuDescriptor` a `Restoring` that `build_account_descriptor`
  answers before `authenticated`, and draw it as one disabled item, *Checking your keychain…*.
  Test the descriptor and the drawn item (text, disabled) beside `mod.rs`'s
  `signed_out_the_section_offers_sign_in`.

- **The log level can only be set by an environment variable.** The host reads `KUBETAIL_LOG_LEVEL` in
  `env_filter` (`src-tauri/src/logging.rs`). The sidecar reads the same variable in `main.go`, and
  inherits it from the host that spawned it. Someone who starts the app by double-clicking it cannot
  set either one, so the level can only be changed by running the app from a terminal. **Fix:** store
  the level in `host.json` (`src/lib/host-file.ts` owns the read/update/subscribe protocol) and add a
  picker to the settings dialog next to the color-scheme one. Let the environment variable win when
  it is set, so it stays the dev override. Pass the level to the sidecar by setting it on the child
  process at spawn: it stays an environment variable, so the "every endpoint is an argument" rule in
  `sidecar/CLAUDE.md` and its `config_test.go` pin are unaffected. **Decide as part of it:** whether a
  change takes effect immediately or on the next launch. Immediate needs a `slog.LevelVar` in the
  sidecar and a way to poke it; on the host it needs the `EnvFilter` wrapped in its own
  `reload::Layer` and that handle kept in `AppState`. The host's subscriber already reloads a *layer*
  (that is how the log file arrives after `setup`), but a layer handle cannot change a filter — this
  is a second, separate handle beside it. Next launch needs neither, and is probably enough — the
  level is normally raised before reproducing a problem, not during one.

- **A record says what happened, never where in the code it came from.** The sidecar's startup
  line carries `version` now; the host's carries none, and neither process records a call site.
  The gap is widest in `sidecar.log`: `log/slog` has no module concept, so a record there is
  timestamp, level, message and fields — nothing says which of the sidecar's packages emitted it.
  `main.log` at least carries `target`, the emitting module.
  - **The host's version stamp** is `CARGO_PKG_VERSION` on its startup line.
  - **The call site** is one option per language: `AddSource: true` on the sidecar's
    `slog.HandlerOptions`, and `.with_file(true).with_line_number(true)` on the host's fmt layers.
    The host's is free — `tracing` bakes file and line into a static `Metadata` — while slog's runs
    `runtime.CallersFrames` per record, which does not matter at the rate we log.
  - **Trim the path, or it costs history.** With `-trimpath` slog reports
    `github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/caches.go` — 64 characters
    before the key, roughly half again the size of a typical record, against a 2 MB rotation. Cut
    the module prefix in `hostKeys` and it is ~20%. (A `go test` build reports an absolute path
    instead, so nothing should assert the shape.)
  - **Flatten slog's `source`, and match the host's keys.** It arrives as a nested
    `{function, file, line}`, and `logs.rs`'s `parse_record` folds fields it does not know into the
    message as `key=value` — so a dev terminal would show the whole object. `hostKeys` is where to
    flatten it to the host's `filename`/`line_number`, keeping the two files one shape.
  - **`webview.log` opts out**, the way it already opts out of the target: every line's call site is
    `webview_log.rs`'s `emit`, a constant. Forwarded sidecar lines in `main.log` are the same shape
    of noise — they would point at `logs.rs`, not at the Go frame the panic text already names.
  - **Redaction is unaffected**, but only by luck: `redactHandler.Handle` copies `r.PC` into the
    record it builds, so the source is derived by the inner handler and never reaches `redactAttr`.
    Without that copy it would log as `<unrendered *slog.Source>`. Say so in the comment when
    turning this on.

- **An off switch for memory and KubeQuery.** Lands before a release build ships
  [memory](security/2026-09-24-memory.md) or [KubeQuery](security/2026-09-27-kubequery-reads-the-mirror.md).
  A setting in the Settings dialog, stored in `host.json` with the others. Off, a turn is offered
  no `Memory` tool and its `<context>` block carries no `## Memory` section, and a turn is offered
  no `KubeQuery`. The notes stay, and the dialog still lists and edits them. The sidecar
  never reads `host.json`, so the host passes the value over the gRPC control channel
  (`proto/`), on connect and on every change. This is the first setting the sidecar reads, so
  the message it adds is the one later settings reuse.

- **How memory grows.** Memory is cut to a name and a body, and each step below adds to it without
  undoing it. Each waits for its reason
  ([ADR](adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md)).
  - **Flag notes from before a rebuild**, when a recreated cluster leads the model astray. Every
    write already stamps `server_uid`. Decide then what a throwaway kind or minikube cluster does.
  - **Find notes by UID across contexts**, when a renamed or aliased context loses its notes. Add
    an index on `server_uid`.
  - **An index and a `read` op**, when notes outgrow the section. Add `description` then, and
    `type` if some kinds of note should stay whole in the context while others do not.
  - **Versions**, if the dialog and the model are ever seen overwriting each other.
  - **Tie a note to a namespace or object**, so the dashboard can bring up the notes for what is on
    screen and flag a note whose namespace is gone.
  - **Revisit the limits** (500 bytes a note, 4 KiB a scope) after use. Any note change resends the
    card and the whole section, up to about 12 KiB, on the next question.

- **Recent webview errors on screen.** `ConnectionStatus` shows only the newest bus error and
  dismisses it after 5s. Retain errors in a bounded ring buffer behind a "Recent errors" list in
  the settings dialog. A shipped build has no inspector (`debug-prod` must not ship).

  `error-bus.ts` is the shared producer: the host-log forwarder persists its errors,
  `ErrorBoundary` reports render crashes with component stacks, `ReadyGate` reports startup
  failures, and global handlers report uncaught errors/rejections. The history should subscribe
  to this bus too. Module-load failures before `main.tsx` runs remain outside it.

## Frontend (webview)

- **`AppLogo` has no user.** No component renders `src/components/primitives/app-logo.tsx`,
  and `design/components.json` still maps it to its Figma component. Decide whether the shell shows the logo again; if not, delete the file and its
  manifest entry, and retire the Figma component.

- **`PageHeader` is only a note.** `src/components/dashboard/page-header.tsx` draws one muted
  paragraph, much as `Note` in `src/routes/dashboard.tsx` does. Decide whether the design keeps it
  as its own component; if not, fold the two into one and update its `design/components.json`
  entry.

- **The dashboard tables' scrollbar ignores macOS's setting.** The tables draw a custom bar
  that always shows (`table-scrollbar`, `index.css`), since WebKit draws the rows' separators
  over its overlay bar. Moving the separators onto the cells (`border-separate`) did not stop
  it. The log viewer, with no row separators, keeps the system's bar. Find what draws over it,
  and let the tables take the system's bar again.

- **A pod's node facts in the Pods table.** OS, Arch, Region, Zone and instance type are the
  node's, not the pod's: its `kubernetes.io/os`, `kubernetes.io/arch`,
  `topology.kubernetes.io/region`, `topology.kubernetes.io/zone` and
  `node.kubernetes.io/instance-type` labels. The Pods page would watch `v1/nodes` beside its pods
  and look each pod's `spec.nodeName` up, offering those as hidden-by-default columns in the
  column picker (`objects-table/columns/pods.tsx`). The same lookup draws a pod's node name in red while that
  node is not Ready, as the mockup's Pods frame does. The cache already holds the nodes.

- **The object panel watches whole streams for one object.** Its YAML tab holds an `unshared`
  `clusterCachedDataObjectCollectionWatch` of the object's kind, and its Events tab an `unshared`
  `clusterCachedDataEventCollectionWatch` of the whole cache, each beside the page's own. **Shape:** watches
  scoped to one object (by `uid`, or namespace and name), which the pod detail page (dashboard
  spec, step 4) wants too.
- **An object's Events tab also shows an earlier object's events.** The events watch names an
  event's object by kind, namespace and name, so a pod deleted and recreated under the same name
  shows both pods' events (`object-panel/events-view.tsx`). **Fix:** add `involvedUID` to the
  sidecar's cached event and match on it.
- **No way from an object's events to the Events page.** The object panel's Events tab lists the
  object's events alone; the Events page has no filter to open it on. **Shape:** a filter on the
  Events page (by object, and text like the kind tables'), and *Open in Events* at the foot of the
  tab.
- **A dashboard log view cannot move to a window.** The object panel's Logs tab
  (`object-panel/logs-view.tsx`) has no *Open in window*: a log window is keyed by a chat and its call
  (`LogWindowsProvider`, the host's `log_windows.rs`), and this view has neither. **Shape:** a
  window keyed by its view, so any view can open one. The pod detail page's Logs tab (dashboard
  spec, step 4) will want the same.
- **An object's Logs tab reads every container.** A pod with a sidecar (`istio-proxy`) mixes its
  lines in. **Shape:** a container picker in the tab, from the pod's spec, feeding the source's
  `containers`.
- **A table cell cannot be copied.** The kind tables draw names, IPs, images and labels the user
  wants on the clipboard, and a row selects no text usefully. **Shape:** a copy affordance on a
  cell's hover, writing the full value (not the truncated one) through `copyToClipboard`
  (`src/lib/clipboard.ts`); which columns offer it is a design call.
- **No cluster writes from the dashboard.** Restart (rollout), scale, delete, cordon, uncordon and
  drain a node, suspend or trigger a CronJob, roll back: the mockups draw several, and the sidecar
  has no mutation that writes a Kubernetes object. Each is a new mutation and a security change,
  since today a cluster write reaches the cluster only through an approved command
  (`docs/security-model.md`). **Shape first:** whether a write from a click asks as the model's
  does, and how a destructive one confirms.
- **No debug terminal for a pod.** The mockups' `Pod detail / Debug terminal`: an exec (or
  ephemeral debug container) session from the pod's row or detail page. It needs an exec stream
  in the sidecar and the exec window the security model anticipates (`src/CLAUDE.md`, *Security
  invariants*: no HTML interpretation, control sequences stripped).
- **A Secret's values cannot be revealed.** The sidecar redacts Secret data when it writes the
  cache, so the table and YAML hold `[redacted]` and the webview has nothing to show. A reveal is
  a live read of the one Secret, and Secret data is a permissioned read
  (`docs/security/2026-10-05-secret-data-is-a-permissioned-read.md`): a click reading it is a
  security change, recorded under `docs/security/`.

- **The log viewer shows only the filters the model set.** Kubetail's sidebar lists each region, zone, OS, architecture and node the open sources span, with a count per value. That needs one query answering what a view's sources resolved to, which the sidecar's resolve already computes (`services/logs/resolve.go`) and the wire does not carry.
- **A workload's log view has no switch to hide its gone pods or its earlier instances.** A
  Deployment's view shows every pod the Deployment runs or ran within the log file's retention, a
  replaced pod's lines marked previous (`services/logs/fetch.go`, `fileOnlyStreams`), and that is
  the right default: a rollout's history is what the user came for. Two things it should let the
  user turn off, each a filter on the scope the sidecar applies as it applies the node filters, with
  a control in the viewer's filter bar: pods the mirror no longer holds, and instances before a
  container's current one. The second also wants the sidecar to read a live workload pod's earlier
  instances from the file as a Pod source's are read (`fileRows` with `t.current` filled for the
  workload's pods, `podSources` fills it for Pod sources alone today), so that a workload's view
  shows a restarted container's earlier instance by default and the filter has something to hide.
  **Trigger:** the first user who wants a Deployment's live pods alone.
- **The inline card reads an `At` window and keeps one half.** `src/lib/inline-log-lines.ts` asks
  for both halves of the moment and drops the other, a read for lines nobody uses. The library's
  `Since` and `Until` each read one half as one first page; the card still asks `At`.
- **Read on through empty log pages.** A grep with no match across many logs returns several empty
  pages in a row, each cut at the page's limits (→ [ADR: backward log reads assume normal
  logs](adr/2026-10-18-backward-reads-assume-normal-logs.md)), and the viewer shows *Keep looking* after each. It could read on by itself a few
  times before asking, so the limits' size no longer reaches the user. The next of these to do.
- **The log viewer cannot say how far a search has got.** *"Searched back to 09:14, no matches
  yet."* The webview never reads a cursor, so a page needs to carry a moment: a small wire change.
- **The log viewer cannot tell a limit from a failure.** A stream stopped at a second too loud to
  read (`FailLimit`) and one whose read failed both arrive as a `stream-skipped` notice. Telling them apart lets the
  kubetail offer sit next to the reason.
- **The log viewer has no download or timezone setting.**
- **The log file's budget has no setting.** Each cluster cache's log file holds up to 1 GiB (`logstore.DefaultRetention.Bytes`), with SQLite's index and WAL on top, so a user with several clusters can use several GiB of disk. **Trigger:** a user asking to keep less. **Shape:** a setting read where `logs.New` takes its `Retention`.
- **The log viewer draws no notice at a stream's start when the kubelet rotated the start away.** A hole between two known lines is exact: the kubelet returns the line before the hole when it still has it, and a later first line when it does not (→ [ADR: a head read never links](adr/2026-10-17-a-head-read-never-links.md)). The start of a stream has no line before it, so a quiet start and a rotated one look the same. A guess from `started_at` ("the container started well before its oldest line") would mark nearly every log, since most containers boot for seconds to minutes before their first line. **Shape:** a stronger signal than time alone, such as the stream's bytes read from its beginning being near the kubelet's rotation size, or an archive backend that answers the question properly. **Trigger:** a user reading a busy container's rotated start as its real start.

- **The omnibox's TLS badge misses a schemeless server.** `tlsUnverifiedReason` (`src/lib/kube-config.tsx`)
  reads the entry's `server` for an `http://` prefix, but client-go defaults a schemeless server
  (`localhost:8080`) to plain http unless the kubeconfig names a CA or a client certificate or skips
  verifying — so such a cluster dials unencrypted and wears no badge. The sidecar's cluster card
  resolves the scheme through `rest.DefaultServerURL` (`clustercard.tlsPosture`) over two presence
  bits the record now carries (`HasCertificateAuthority` on the entry, `HasClientCertificate` on
  the user half); the wire exposes neither yet. **Fix:** add both to `schema.graphqls`
  (`ClusterStatusSourceKubeconfigClusterEntry`, `ClusterStatusSourceKubeconfigUser`), `pnpm codegen`,
  and let `tlsUnverifiedReason` take the same rule — the two mappings should agree.

- **Swap the code-block highlighter to Shiki.** `src/lib/highlight.ts` runs highlight.js through `lowlight`: regex grammars that visibly misfire on nested YAML, template strings and JSX, and a hand palette in `markdown.css` rather than an editor's theme. Shiki is VS Code's own TextMate grammars and themes, so a manifest highlights the way it does in the editor. The render path stays as it is — `codeToHast` yields the same hast `hast-util-to-jsx-runtime` already renders, so no HTML string enters, and the change is contained to `highlight.ts` plus the `Code` component in `markdown/markdown.tsx`. **Weigh:** the engine is Oniguruma compiled to WASM (~600 KB) or the pure-JS `@shikijs/engine-javascript` (lighter, weaker grammar compatibility), and grammars run 2–5× highlight.js's — a heavier baseline than today's 22 KB even lazily loaded; tokenizing is async-only, so a growing block re-highlights through a promise rather than the synchronous `highlight` call; and dual-scheme colours become `themes: { light, dark }` emitting both as CSS variables per token, which replaces the `--hl-*` palette rather than reusing it. **Trigger:** the first highlighting misfire someone notices on a manifest, or an editor-matching theme being wanted.

- **The new dashboard is unfinished** ([#881](https://github.com/kstackhq/kstack/issues/881), from
  the *K8s Dashboard (imported)* page of the
  [Kubetail Figma file](https://www.figma.com/design/TeS8ZaScKc01RDfYJ06A8N/Kubetail)). The sidebar,
  the namespace scope, the page header, the Overview and the kind tables are built. Still to build:
  - **A pod detail page** (`Pod detail / Overview`, `Pod detail / Logs`): a route for one object,
    with a header (status, owner, node, IP, age, restarts) and Overview, Logs, Events and YAML
    tabs. The route never builds or parses an `ObjectID`.
  - **The data gaps** not filed above (*Namespaced watches* and *A health verdict* are, under
    *Sidecar — cluster service*): object search across kinds, for the sidebar's search; usage
    metrics, for CPU and Memory columns; resource requests across the cluster; cluster facts
    (Kubernetes version, provider, region, upgrades); external sources (Prometheus alerts, Argo CD
    syncs). A view that meets a gap draws without it, never with placeholder numbers.

- **No keyboard jump to a dashboard page.** The mockups' kind switcher (`Kind switcher menu
  (open)`) jumped to any kind from the keyboard; the dashboard leaves it out, since its rows are
  the sidebar's. A window-wide jump (⌘K) over the sidebar's pages and kinds would bring the
  keyboard path back. `ScopeMenu` (`dashboard-resource-nav/scope-menu.tsx`) already has the list,
  search and keys.

- **The dashboard's namespace scope holds one namespace.** The sidebar's namespace picker picks one
  namespace or all of them ([ADR](adr/2026-11-12-the-namespace-scope-is-a-search-param.md)). Let it pick
  several: the `namespace` param becomes a list, the picker's menu gains checkboxes, the tables
  filter by the set, and the sidebar's *Namespace* heading names the count when there are more
  than one.

- **One localStorage hook for JSON chrome.** `useTableColumns` (`src/lib/table-columns.ts`) and
  `useLogViewerColumns` (`components/logs/log-viewer/columns.ts`) each carry the same
  read-with-fallback and save-in-try/catch code; `usePersistedFlag` and `usePersistedWidth` are two
  more copies. They already differ: `usePersistedFlag` re-reads when its key changes,
  `useTableColumns` reads once per mount, so it shows the right kind's columns only because
  `dashboard.tsx` keys `ObjectsTable` on the kind. **Fix:** a `usePersistedJson(key, parse,
  fallback)` beside `persisted-flag.ts` that re-reads on a key change, under both column hooks.

- **Two column pickers.** The log viewer's toolbar (`log-viewer/toolbar.tsx`) has its own Columns
  popover of checkbox rows; `ColumnPicker` (`components/dashboard/column-picker.tsx`) does the same
  and adds reordering and *Reset*. Their triggers already differ (a ghost icon button against an
  outlined one). **Fix:** the log viewer takes `ColumnPicker`, with reordering optional or on, or
  the two share one checkbox row. Whether the log viewer gains reordering is a product call.

- **The objects table's ages have a formatter of their own.** `Age` (`objects-table/columns/common.tsx`)
  draws through react-timeago with its own largest-unit formatter (`3h`), where every other
  length of time is `humanDuration` (`3h20m`), as `src/CLAUDE.md` asks. **Fix:** draw the Age
  column and `ageColumn`'s cells with `Elapsed`, and drop react-timeago if nothing else reads it.

- **A pod's tone has names of its own.** `cluster-health.ts`'s `StatusTone` (`ok`, `pending`,
  `failing`) maps one-to-one onto cluster health's `Tone` (`ok`, `attention`, `error`) through
  `HEALTH_TONE`, so callers translate (`TONE[HEALTH_TONE[b]].text` in `objects-table/table.tsx`), and the
  pill draws its own dot beside `ClusterDot` at another size. **Fix:** type pod tones as `Tone`,
  drop `StatusTone` and `HEALTH_TONE`, and draw the dot with `ClusterDot`.

- **One gate for a watch's not-synced and loading states.** `ObjectsTable`, `EventsTable`, and
  `MissingKindPage` in `routes/dashboard.tsx` each draw "not synced" without an
  active cache, a spinner until the `Bookmark`, then their content, and their wording has drifted
  (*Loading…* against *Loading pods…*). **Fix:** one component (`<WatchGate active phase noun>`)
  that draws the note or the spinner, else its children, so the never-empty-before-the-`Bookmark`
  rule lives in one place.

- **A table's density is split between two files.** `VirtualTable`'s `TALL_ROW` sets its cells'
  vertical padding, since the row's height depends on it, while `ObjectsTable` sets their
  horizontal padding and the header's height with descendant selectors on its card (`HEADER`,
  `CELLS`). `EventsTable` keeps the library's spacing, and a third table would copy the selectors.
  **Fix:** `VirtualTable` takes the density, on `RowSize` beside `className` and `px`, and sets all
  of the cell padding and the header height itself. Whether `EventsTable` takes the tight one is a
  design call.

- **Evaluate moving provenance down into `useFoldedSubscription` (keyed on urql's operation key).** `useCacheCollectionWatch`'s provenance is a caller-derived string compared against server-echoed fields (`cacheID` + `apiVersion`/`resource` on `ClusterCachedDataObjectCollectionWatchFrame`). But one layer down, `useFoldedSubscription` already computes `createRequest(query, variables).key` — which changes *exactly* when the watch's variables change, i.e. it is precisely `(cacheID)` for kinds/events and `(cacheID, apiVersion, resource)` for objects, derived automatically for any subscription. And that file already implements this same tag-fold-mask pattern for the sibling dimension (`generation`, for reconnects). **Fix:** make `Generational<Result>` carry `{ generation, key, result }` and gate both the reducer's fold and the exposed-`data` mask on `key` as well — then `currentProvenance`, `DeltaFrame.provenance`, `joinProvenance`, *and* the `apiVersion`/`resource` schema echo fields all disappear, and every future keyed watch gets staleness protection for free instead of re-spelling it. **Weigh first:** server-echoed provenance additionally defends against a genuinely *mis-routed* frame (a sidecar/host-bridge multiplexing bug), which a client-side key cannot — nothing currently claims that as a motivation, so if it *is* wanted, say so where the fields are defined, since it's the only thing justifying the schema surface. Also note the current design's failure mode is silent: a mismatch between the two hand-spelled provenance strings drops every frame with no type-level protection.

- **Extract a shared collection-watch test harness; mock `useActiveCluster` instead of its inputs.** `cluster-cached-data-events.test.tsx`, `cluster-cached-data-objects.test.tsx`, and `dashboard-nav.test.tsx` each carry a near-identical block: a `vi.hoisted` `useFoldedSubscription` stand-in with `statusState`/`pushReset`, a **re-implementation of the real `collectionWatchReducer`** inside `vi.mock('@/lib/clusters')`, a `clusterFixture(hasCache, cacheId, serverUid)`, `pushFrame`, and the same `beforeEach`. Two costs: the fake `applyChange` is a second implementation of `src/lib/clusters.tsx`'s real one and can drift from it while tests still pass; and each file still mocks the **pre-refactor** seams (`clusters` + `active-kube-context`) when `useActiveCluster()` is now exactly the seam they want — mocking it deletes `clusterFixture` and the `active-kube-context` mock from all three. The provenance straggler/swap cases are now covered once in `use-cache-collection-watch.test.tsx`; the copies in the events and objects suites can go.

- **A component is placed where it is to dodge a transport gap.** urql shares one operation between identical subscribers and its replay is query-only, so a second subscriber to a live subscription sees nothing until the next frame. The panel works around it by structure — hoisting `useClusterCacheStatsStateWatch` to `ClusterRow` — enforced only by a long comment. **Cost:** component placement is load-bearing and silently re-breakable by any future consumer. **Fix:** put replay in the layer that owns the connection — have `subscribe-exchange.ts`/`useFoldedSubscription` keep the latest value per `operation.key` and hand it to a late subscriber on attach.

- **`clusters.tsx`'s join memo rebuilds every cluster on every health frame.** The memo depends on `healthMap`, so a sync-health frame (re-emitted per changed cache on a periodic tick) produces new identities for *all* clusters and *all* joined caches, invalidating every downstream memo keyed on them; the cache lookup inside is a linear `caches.find` per cluster on top. **Fix:** build a `Map<clusterID, cache>` once, and split the join — memoize cluster+cache on `[clusterMap, cacheMap]` only, then attach `health` per row in a child subscribing to just its cache's entry, so one moved cache re-renders one row.

- **`useCachedKinds` folds ~150 records into a map and array to yield two lookups.** `SyncDetail` uses the list twice and never as a list: `timelineSyncFor` picks one record's `{ id, resource }`, and `idOf` does a linear `find` per failing kind to map a GVR back to its record id. **Fix:** return a `Map` keyed by `gvrKey` alongside the timeline record, so both lookups are O(1) and neither needs the array. Collapsing the hook to just the timeline record does not work — it would leave `idOf` with nothing to search.

- **`cluster-sync-panel/panel.tsx` is ~1150 lines holding five separable units.** Ten `graphql()` documents and five subscription hooks (the event timelines, the cache's sync status and its kinds), a tone/time/formatting vocabulary, and three independent panes (`ConnectionDetail`, `SyncDetail`, `ClusterRow`) plus the dialog shell. What the status popover shares is already out: its streams in `src/lib/cluster-diagnostics.ts`, the sync wording in `src/lib/cluster-health.ts`, and `RelativeTime`, `Countdown` and `RetryButton` as top-level components in `src/components/primitives/`. **Fix:** the hooks and vocabulary to `src/lib/`, and `connection-detail.tsx`, `sync-detail.tsx` and `cluster-row.tsx` beside `panel.tsx`, leaving the panel file as a ~110-line shell. Pure churn, so worth doing when the file is next touched substantively rather than on its own.

- **Investigate consolidating the small `src/lib` modules into a shared `util.ts`.** `src/lib` has grown a tail of very small files — `gvr.ts` (27 lines), `platform.ts` (31), `dashboard-nav.tsx` (32), `active-cluster.tsx` (42), `error-bus.ts` (50), `window-maximized.ts` (58), `active-kube-context.tsx` (60) — and the per-file navigation/import overhead is starting to outweigh the separation. **The distinction to make first, before moving anything:** most of these are *not* generic utilities, they're cohesive domain modules that happen to be short — `active-cluster.tsx`, `dashboard-nav.tsx`, `active-kube-context.tsx`, `connection-status.tsx` are React hooks/providers with a clear single responsibility and their own consumers, and folding those into a grab-bag would trade a real boundary for a line count. The genuine candidates are the **pure, dependency-free helpers**: `gvr.ts` (a type + one key function) and `platform.ts` (sync `isMacOS()`/`isLinux()` UA checks); `window-maximized.ts` and `error-bus.ts` are borderline. **Tradeoffs to weigh:** a shared `util.ts` is a magnet — it accretes unrelated helpers, blurs ownership, and (if it ends up importing React/urql/domain modules) can create import cycles that the current leaf files structurally can't; against that, a dozen 30-line files means more files to open to follow one flow. **Output:** either a small `util.ts` holding only the pure leaf helpers (with a rule for what may go in), or a decision to leave the split as-is and treat file count as acceptable — recorded either way so this doesn't get re-litigated.

- **Investigate urql caching — are we using it correctly?** We've never audited how the urql client is configured (`src/lib/graphql/client.ts`) w.r.t. caching. Confirm which cache is in play (the default **document cache** vs. `@urql/exchange-graphcache`) and whether it fits our access patterns: queries/mutations over Tauri IPC (`invoke-fetch.ts`), and the many subscriptions that maintain their own reduced state via `useFoldedSubscription`. Questions to answer: does the document cache help or just add staleness for our mostly-subscription data; are mutations correctly invalidating/refetching the right queries (e.g. cluster enable/sync toggles vs. `clusterCollectionWatch`); is `requestPolicy` set intentionally; and would normalized caching (graphcache) actually buy us anything given watches already own the live state. Output: a short note on the current behavior + whether to change the exchange pipeline or leave it.

- **Tabs would collide on the history ceiling.** `kubetail:history-forward-ceiling` (`history-nav.tsx`) is
  one key in `sessionStorage`, correct while a window is one webview: each webview has its own store,
  and the value describes that webview's own history stack. If a tab is ever a second router inside
  **one** webview rather than a webview of its own, two tabs would overwrite each other's ceiling and
  Forward would light up on the wrong one. The fix then is a per-tab key; the `__TSR_key` tag catches
  a mismatch and yields 0, so the symptom is a Forward that goes missing, not a broken one. The
  chrome's shape (`kubetail:sidebar-*`, `kubetail:right-sidebar-*`) is deliberately app-wide and needs
  nothing here.

- **Startup URLs don't reference the active kube context.** A fresh window lands on a bare `/chat` (`index.tsx` redirects `/` → `DEFAULT_ROUTE` with no search); `useActiveKubeContext` resolves the context by *falling back* to `kubeConfig.currentContext` but only *writes* `?kubeContext=` on an explicit pick. Consequence: the landing URL isn't self-describing or deep-linkable until the user interacts, and two windows on different default-resolved contexts look identical in the URL. Fix: seed `kubeContext` from the resolved current-context at boot (e.g. `index.tsx` redirect or an `_app` `beforeLoad`). Catch: at `beforeLoad` the clusters stream may not have delivered its first frame, so current-context isn't known synchronously — either accept a sometimes-omitted param or resolve+write once after the first frame lands.

- **Consider holding the window's watches in Jotai atoms.** `ClustersProvider` (`src/lib/clusters.tsx`)
  and `ModelsProvider` (`src/lib/models.tsx`) each hold watches for the whole window in a provider
  mounted in `__root.tsx`, and each window-wide watch adds another provider to that stack. Each
  provider's value is one context, so a reader re-renders on any change to that value. **Explore:**
  one atom per watch, opened the first time something reads it, with derived atoms for the joins
  (caches onto clusters, the health keyed state watch onto caches) so a reader re-renders only for what it reads.
  **Weigh:** a new dependency, and fitting `useFoldedSubscription`'s generation gate and batching into
  an atom's lifecycle. A late reader must still get the snapshot, which a provider guarantees today.
  Tests that mock `useClusters` or `useModels` would need a store instead.

- **Nothing checks that the Figma designs keep up with the code.** [`design/components.json`](../design/components.json) records each component's source blobs at its last sync ([`design/CLAUDE.md`](../design/CLAUDE.md#drift)), and `make status-desktop-design` lists the designs behind their code. Nobody is made to run it. Given `FIGMA_TOKEN`, it reads the design side over Figma's REST API. **Shape:** `tools/designsync` reports the components a pull request's changes left behind. It runs on a pull request touching `src/components/` or `src/index.css`, report-only, like `coverage / Gate`. **Needs:** a Figma access token as a CI secret, read-only.

## Security

The current picture — boundaries, and which protections a test actually pins — is
[`security-model.md`](security-model.md); the findings behind these items are
[4 September review and disposition register](security/2026-09-04-security-review.md), which
accounts for every finding from the [2 September threat model](security/2026-09-02-threat-model.md).

A risk we decide to accept is recorded as a **By decision** row in
[`security-model.md`](security-model.md), each linking the ADR that accepted it — so an accepted
risk stays distinguishable from an unnoticed one, and is not repeated here.

- **Learn a static-key AWS login's account from STS (medium; sidecar owner).** With static
  keys the SDK gives no account id, so `awsproxy` decides every change as an unknown account's
  (each asks) and sends no `X-Amz-Expected-Bucket-Owner`; a bucket changing hands within the
  bucket list's minute is read unasked. Ask `GetCallerIdentity` once per access key and use its
  account wherever `accountOf` reads one
  ([#1224](https://github.com/kstackhq/kstack/issues/1224)).

- **Refuse a macOS directory whose ACL grants another account write (low; host owner).**
  `src-tauri/src/paths.rs` makes each of Kubetail's directories owner-only by its mode bits: it
  sets 0700 (`ensure_data_dir`), or leaves the config directory and the app folder alone when
  they already have it (`is_owner_only_dir`). On macOS an extended ACL is apart from the mode, so
  an existing directory carrying one that grants another account write keeps it at 0700, and its
  entries stay replaceable. Adding the ACL needs write access to the user's own
  `~/Library/Application Support` first. Linux is not exposed: a POSIX ACL's mask is the group
  bits, which 0700 clears. Read the ACL and refuse or strip a granting entry, and record the
  protection in [`security-model.md`](security-model.md).

- **Decide what to do about colour that hides a log line from the eye (low; logs owner).** A
  kept SGR sequence can set a foreground that matches its background, or the theme's own
  background, or dim the text, and the viewer draws it as asked
  ([log lines keep their colours](security/2026-10-19-log-lines-keep-their-colours.md)). Copy,
  grep and the model read the line stripped, so the text hides from the eye alone. Dropping
  `background-color` leaves the theme-matching foreground open; a contrast floor on every kept
  colour would close it. Either land one or accept it as a **By decision** row with an ADR.

- **Let a link lead a Windows layout folder out of the profile (low; sidecar and host owners).**
  A layout folder a link leads elsewhere must resolve inside the user profile, whose ACL keeps
  it private
  ([ADR](adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md)).
  A junction to another drive fails the start until Kubetail sets an ACL on such a folder itself.

- **Check who can write the folders above Kubetail's own (low; sidecar owner).** Kubetail assumes
  every folder on the path to a layout folder, and to a link's target, is writable only by the
  user or root, and checks only the Unix runtime folder. A root under a folder another account
  can write lets that account repoint a link Kubetail follows.

- **Measure what a run's check costs (easy; sandbox owner).** A run's check names each rule path
  as the kernel does (`Policy.compile`, `sidecar/internal/run/sandbox/policy.go`): Linux follows
  its links, macOS asks the kernel for its full path, and the check for a folder named two ways
  looks up its ancestors' file identities. Benchmark it by rule count, path depth, shared
  ancestors and lookup latency before proposing a cache
  ([#1150](https://github.com/kstackhq/kstack/issues/1150)).
- **Pin path comparison on a Linux mount that folds names (low; sandbox owner).** Paths are
  compared as files (`realpath.Under`), and a policy naming one folder two ways is refused
  (`aliased`, `sidecar/internal/run/sandbox/policy.go`), but no test mounts a folding filesystem
  (vfat, a casefold directory): the Linux tests stand a hard link and a fake in for one. Run the
  sandbox suite on such a mount in CI, and pin there that `filterPath`
  (`sidecar/internal/services/settings/filter.go`) drops a second spelling of a `PATH` folder.
- **Audit what secrets a sandboxed read still passes (medium; sandbox owner).** Redaction covers
  a Secret's values, `last-applied-configuration`, and a helm release's values and manifest
  Secrets. Check what else carries a secret to the model: env values and ConfigMaps; a helm
  release's other rendered objects, rendered notes, chart defaults and files; Secrets typed
  `helm.sh/release.v1` by hand; helm releases kept in ConfigMaps (`HELM_DRIVER=configmap`, Helm
  2's Tiller); and the state of tools that hold credentials — Argo CD, Flux, sealed-secrets,
  external-secrets, cert-manager — and CRDs that carry them. Each finding is redacted or recorded
  as a **By decision** row.

- **Let a sandboxed command request a service account token (medium; sandbox owner).** The
  proxy refuses the `token` subresource of `serviceaccounts`, since the answer is a credential
  and anything a command prints reaches the model and then the provider. **Shape:** a token
  reaches the command through code Kubetail runs, never through output the model reads, so
  `kubectl create token` and a TokenRequest a tool makes work in the sandbox. Landing it moves
  the refusal's row in `security-model.md` and is a security record.

- **Check Secret redaction by hand against a real cluster (medium; sandbox owner).** On `pnpm
  tauri dev` against a cluster with a helm release, in `Ask` mode: run `kubectl get secret <any>
  -o yaml`, deny its *Show Secret* request and read `[redacted]`; run `kubectl get secrets -A |
  head` and read a table with no request; run `helm list -A`, deny each request, and read the
  releases; run `helm get values <release>`, deny, and read `[redacted]` for every value. Then
  allow the `-o yaml` read for this chat and read the values, and run `helm upgrade` with its
  reads allowed for the command and read its writes ask as cluster writes. Run the helm commands
  with a Helm 3 and a Helm 4 binary, since the rewriter assumes they share the release Secret's
  encoding.

- **Check cluster writes by hand (medium; sandbox owner).** On `pnpm tauri dev` against a kind
  cluster, on Linux or macOS, ask "delete pod x" and see the request *Delete pods/x in default on
  kind-kind* with its path and the command under *Sent by*; deny it and see the model read the
  `Forbidden`; ask again, approve it once, and see the pod go. Ask for `kubectl scale deploy x
  --replicas=2` and read a diff whose `replicas` line changes, the patch under *Show the
  request*; press *Allow for this chat*, ask to scale it back, and see it run unasked, the calls
  tagged `approved · this chat` and `allowed`; remove the rule from the composer's *Allowed for
  this chat* and see the next scale ask. Ask for `kubectl delete ns test` and see Approve once and
  Deny alone. Apply a Secret with one value changed (`kubectl apply --server-side`) and read
  `[redacted: changed]` on its key. Create three pods, ask for `kubectl delete pods --all`, press
  *Allow for this command*, and see the other two go unasked. Then ask for a `kubectl scale` with
  a short `timeout`, leave the request unanswered, and see it drawn `not answered` once the call
  ends.

- **Check the Linux sandbox by hand (medium; sandbox owner).** On `pnpm tauri dev` on Linux,
  approve `cat ~/.kube/config`, `curl -sI https://example.com` and `ls ~`, and read each refused
  or empty; approve `kubectl get pods -A | head`, with a `kubectl` that is not a snap, and read
  the pods. On a stock Ubuntu 24.04 desktop, install a release candidate's `.deb`, change
  nothing, and read a sandboxed call's disclosure say `sandboxed`. The release's 24.04 check runs
  the chain, not the app.

- **Check the Seatbelt sandbox by hand on macOS (medium; sandbox owner).** On `pnpm tauri dev`,
  approve `cat ~/.kube/config`, `curl -sI https://example.com`, `ls ~`, `ls /tmp`, `security
  find-generic-password -s x`, `open https://example.com`, `pbpaste`, `defaults read
  com.apple.finder` and `mdfind kubeconfig`, and read each refused or empty; approve `git
  --version` and `kubectl get pods -A | head`, and read each work, with `kubectl` from Homebrew
  and, where there is one, from Docker Desktop. A rule or Mach service a listed program needs is
  added to `profile_darwin.sb`.

- **Check the Linux sandbox by hand in WSL2 (medium; sandbox owner).** Native Windows has no
  sandbox, and WSL2 runs the Linux build's
  ([ADR](adr/2026-09-28-native-windows-has-no-sandbox.md)); CI has no WSL2 runner. In a WSL2
  distribution with WSLg, install a release candidate's `.deb` and start Kubetail. Check that it
  runs at all, then run the Linux check above, then `cmd.exe /c whoami` and
  `/mnt/c/Windows/System32/cmd.exe /c whoami`, and read each fail to start; `cat` a file under
  `/mnt/c/Users/<you>` and read it refused. In WSL1, read Bash offered without the flag, every
  call asking, and the probe's reason in the sidecar's log.

- **Capture a real helm release for the rewriter's tests (low; sandbox owner).**
  `sidecar/internal/run/kubeproxy/testdata/helm-release.json` is written by hand in the shape Helm 3
  stores. Replace it with one a real `helm install` wrote, a chart with a Secret and a hook, and
  name that helm's version in `TestHelmReadsARedactedRelease`.

- **Time the Secret rewriter on a cluster with many releases (low; sandbox owner).** Time `helm
  list -A` and `kubectl get secrets -A -o json` in and outside the sandbox against a cluster with
  many releases and their history, since each revision is inflated, parsed and gzipped again and
  the grant's limiter bounds requests, not that work. Record the numbers in
  this item; a command more than twice as slow in the
  sandbox gets its own item.

- **Check the rebuilt web search against the real API (low; chat owner).** On a dev build with an
  Anthropic key, on each Claude model: a question that needs a search draws
  *Searched the web* and *Sources*; a follow-up in the same chat is accepted (the replay); a
  cancel mid-search leaves the query on the `Cancelled` row. Once, with `maxUses` raised to 20: a
  paused turn resumes and settles with an answer, and with `maxPauses` at 0 it settles on
  `pause_turn` and its follow-up is accepted. Record the result as a note on
  [web search](security/2026-09-17-web-search.md).

- **Audit prompt caching (low; chat owner).** [Prompt caching](notes/prompt-caching.md) says how
  each provider caches. First, run `make test-desktop-livecache` with every key, fix each catalog row it
  contradicts (`catalog/providers.go`), and clear the UNVERIFIED cells it settles in the note.
  Three questions ride on it: whether OpenRouter passes `ttl: "1h"` on (without it Claude caches
  for five minutes), whether its top-level mark holds when it serves Claude from Bedrock or
  Vertex (a miss on that entry is the sign; pinning the route needs a per-model body field), and
  whether Gemini's OpenAI-compatible endpoint reports a hit at all (a zero there needs the bill
  to tell a miss from a missing report). Then check two things per provider. First, hit rates in
  real chats, not only the live check's two turns: whether each model reads the transcript from
  the cache turn after turn, and whether Mistral's key helps or splits caches. Second, how long
  the vendor keeps a cached
  prefix against the line the app holds, that a vendor keep a prompt no longer than its cache
  lifetime. DeepSeek keeps it on disk for hours to days, Gemini states no lifetime, and Google
  counts implicit caching as retention. Record each provider that falls outside the line as a
  **By decision** row or drop it from the catalog.

- **Run the live switch check (low; chat owner).** `make test-desktop-liveswitch` walks one chat from
  Anthropic to OpenAI to a Chat Completions vendor (Gemini, else the first keyed) and back, each
  step a real tool round over the steps before it. It has not run with keys. It answers what the
  fixtures cannot: whether the Messages API takes a history `tool_use` naming a tool the request
  does not offer (if not, a foreign round whose tool the target lacks goes as its row's text
  alone), whether the Responses API takes a rebuilt call with no reasoning item, and whether a
  Chat Completions vendor refuses a minted id. Record what it finds on
  [a chat switches vendor](security/2026-09-25-a-chat-switches-vendor.md), and try each Chat
  Completions vendor with a key, not only the walk's one.

- **S-2 — kubeconfig trust and exec consent (high; cluster-service owner).** Newly imported enabled
  contexts are probed automatically, including exec plugins; importing a file is a code-execution
  trust decision. Gate the first probe/plugin run behind explicit approval of the resolved command,
  args, environment and source; invalidate approval when these change. Include token/certificate
  file references and server/proxy destinations in the trust design. Test startup, reload, previously
  unused contexts and approval revocation. The highest open finding, and the one whose shape is
  still a product question. Until then use only trusted kubeconfigs. On macOS the surface is the
  wider one: the process environment is an allowlisted import of the user's login shell
  (`sidecar/internal/run/loginshell`), so the gate must approve a plugin's environment as well as the
  path it resolves to through that PATH, not the ones a GUI launch inherits. The chat's bash
  tool is a second runner of the plugin: an approved command that reaches a cluster runs it
  through the same imported PATH ([record](security/2026-09-18-bash-tool.md)).

- **Scope a cluster command to the chat's cluster by construction (medium; chat owner).** A
  sandboxed command's kubeconfig holds the chat's cluster alone, so what is left is a command run
  outside the sandbox. A command run
  outside the sandbox uses the user's own: the prompts tell the model to name the card's context
  in every `kubectl`, `helm` and `flux` command, and nothing checks it, so a command without
  `--context` runs against the kubeconfig's current context, which need not be the chat's cluster
  ([record](security/2026-09-24-the-cluster-through-the-shell.md)). **Shape:** give each such
  command a `KUBECONFIG` naming the chat's context as current — a file of the chat's own, beside
  the user's, never an edit to theirs — so a command that omits the flag still reaches the right
  cluster, and the approval request says which one. The scoped `KUBECONFIG` is not yet in a spec.

- **An approved command must not run what an unasked write left in the workspace (medium; chat
  owner).** A command outside the sandbox starts in the workspace, and tools read config from
  their working directory. A sandboxed command or an unasked `Write` can leave `.git/config` with
  `core.fsmonitor` set; the user then approves `git status`, and the planted command runs with the
  user's files, credentials and network. The same holds for any tool that reads config or runs
  hooks from its working directory, and for a `PATH` holding `.`. Accepted until this lands
  ([record](security/2026-09-28-bash-runs-in-a-sandbox.md)). **Shape:** a command outside the
  sandbox starts somewhere no unasked write reaches unless the user names the workspace, or the
  request says when the directory holds config a tool would run.

- **A narrower AWS identity.** A sandboxed command reaches AWS as its cluster's profile, or the
  sidecar's default chain, with every permission the login has. Wanted: a read-only role per
  profile that the proxy assumes.

- **More AWS services, a row at a time.** `run/awsproxy/services.go` lists thirteen. Each new
  row names its class 6 reads: `secretsmanager` `GetSecretValue` and `BatchGetSecretValue`;
  `ssm` `GetParameter`, `GetParameters`, `GetParametersByPath` and `GetParameterHistory` with
  `WithDecryption`; `lambda` `GetFunctionConfiguration` and `ListFunctions` (the environment),
  and `GetFunction`, or a refusal, since it answers a presigned URL to the code; `ecs`
  `DescribeTaskDefinition` (the container environment).

- **Skip the EC2 metadata probe when no AWS profile names a credential.** With no AWS
  configuration, the default chain falls through to the EC2 metadata endpoint, which takes
  seconds to fail off EC2, on each sandboxed AWS request. Disabling it outright would break a
  Kubetail on an EC2 machine with an instance role, as the EKS plugin's chain uses one.

- **S3 uploads and buckets the user names.** No file is uploaded to S3 from the sandbox, and a
  login without `s3:ListAllMyBuckets` is asked for every bucket read. Wanted: uploads to a
  bucket the user names in Settings, and buckets named there as the login's own.

- **Let a sandboxed `gh` follow GitHub's download links.** Workflow logs, artifacts, archives and
  release assets answer with a signed link to another host, which a run with network off cannot
  reach, and `gh release download` and `gh run download` dial `api.github.com` themselves
  ([ADR](adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)).
  Following a short list of GitHub's download hosts from the sidecar would make them work, and
  make the proxy a path to those hosts.

- **Git LFS through GitHub's proxy.** Its batch request is a `POST` that only reads, and its
  objects live on another host; the proxy refuses it.

- **Serve a user with no `gh` and no GitHub connector through `git credential fill`.** GitHub's
  token comes from the connector or `gh auth token`; the alternative runs the user's credential
  helper outside the sandbox.

- **Find `gh` where the user's shell finds it, on Linux.** `githubauth` looks `gh` up on the
  sidecar's own `PATH`, which on Linux is not the login shell's: a desktop launch misses a `gh` in
  `~/.local/bin` or linuxbrew, and the log says `gh not found`. Deciding which `PATH` to trust for a
  program run outside the sandbox is the work.

- **Rules, modes and durations for a cloud platform's actions.** Every change a sandboxed command
  sends to a cloud platform asks, Once or Deny: no rule, mode or duration reaches one
  ([ADR](adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)).
  Wanted once a cloud platform's users approve the same change often: a cloud platform's own rules,
  and a read-only mode among them.

- **Draw each cloud platform's switch in Settings.** `sandboxCloudAccessGet` and
  `sandboxCloudAccessSet` are served, and nothing draws them: the switches wait for the new settings
  panel. Each is on until the user turns it off
  ([ADR](adr/2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md)), so today only
  a `sandboxCloudAccessSet` call turns one off. Each row says, for the named cloud platform: sandboxed commands
  read your account without asking; what they read is sent to the model's provider; with network on
  a command can send it anywhere; a change asks first; nothing that answers a credential works.
  Then one line of the cloud platform's own, held by the webview beside its name. AWS's line: *An
  object's contents ask first.* GitHub's line: *Private repositories are read without asking.*
  Google's line: *Log entries are read without asking; an object's contents ask first.* Azure's
  line: *Stored data is not reached; an app's settings and a deployment's parameters ask first.*

- **A narrower Google identity.** A sandboxed `gcloud` acts as the user's active account. Issue
  [#933](https://github.com/kstackhq/kstack/issues/933) asks to consider impersonating a service
  account instead, which needs a setting, a UI and the user's own IAM binding.

- **Google services off the table.** Secret Manager, Cloud Functions, Cloud Run, Deployment
  Manager, regional and mTLS endpoints, Cloud Storage's XML API, batch requests and `logging`'s
  gRPC tail reach nothing, nor do `gsutil`, `bq` and client libraries. Each needs its own reading
  of which answers hold a secret.

- **Google uploads that are not text.** A binary upload, or one past 1 MiB, is refused, so
  `gcloud storage cp` of a binary file does not work in the sandbox. A media upload names its
  bucket and object in the query, so it could be `Unshown` with a sentence naming them; a
  multipart one could not.

- **Azure's data plane.** A sandboxed `az` reaches Resource Manager alone: `az storage blob`,
  `az keyvault secret` and `az acr login` fail with the proxy's message
  ([ADR](adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md)).

- **Microsoft Graph for a sandboxed `az`.** `az ad` fails, and `az role assignment list` prints
  ids without names.

- **An Azure cloud the user registered.** A run serves the three public clouds alone; a user on
  Azure Stack gets no account in the run.

- **A cloud platform switch per chat.** `sandbox.<platform>` is global: a cloud platform on is on
  for every chat. Network is the chat's; a chat about one cluster has no need of another cloud.

- **Build the cluster grant on `runproxy.Grant`.** `kubeproxy.Grant` keeps its own token, life,
  limits and write queue beside `runproxy`'s, so the two copies can drift.

- **A setting that turns a cloud model platform on.** Bedrock, Vertex AI and Foundry are pending
  the LLM rebuild, and when they return, no platform is on until a setting turns it on: a cluster
  credential and a model credential are the same identity there, so the environment is never read
  as consent. The setting belongs to the Settings spec; the platform's own conventions supply
  everything else.

- **A cache stops outliving the user's interest in its cluster.** The one obligation left open by
  [the cache is ordinary application data](adr/2026-09-02-the-cache-is-ordinary-application-data.md):
  the file is protected as well as the kubeconfig beside it, but a token expires and a certificate
  is revoked while the file keeps answering. **Shape:** evict a cache whose cluster has not been
  opened in N days, and clear every cache on sign-out (`services/connectors/kubetailcloud`'s `Disconnect` is the hook) —
  sign-out is the user saying the machine no longer speaks for them. `kubestore`'s `Manager.Remove`
  already owns the teardown (closes the file, unlinks it and its `-wal`/`-shm`, refuses a later
  open of the same id), so eviction is a policy above it rather than new machinery; what it needs
  is a last-opened timestamp per cache, written where the cache record lives rather than inside the
  file it is about to delete. **The open question is N**, which trades a cold relist on return
  against how long a revoked credential's answers persist — settle it, and say why, before
  building. **Constraints:** eviction is not a failure, so nothing may render a missing file as an
  error state; and a claim still out must not resurrect the file, `Remove`'s retirement discipline
  (decision recorded first, unlink retried after) being the reference. **When it lands:** move the
  *"A retention policy…"* row in [`security-model.md`](security-model.md) to **Enforced**, naming
  the test, and fold the policy into `sidecar/CLAUDE.md`.

- **R-05 / S-9 — nothing bounds what one request can cost (medium; sidecar/desktop owners).** The
  bounds we have all measure the same thing: bytes and time on a single HTTP request. Four things
  have no bound at all. **Query shape** — gqlgen runs with no complexity or depth limit, so a deeply
  nested query costs whatever it costs. **Concurrency** — any number of operations and subscriptions
  can be open at once; the host's registry is an uncapped `HashMap` and the sidecar counts nothing.
  **Kind fan-out** — a cache arms one worker per discovered kind, and a cluster can have hundreds.
  **Total disk** — the 2 GiB ceiling is per cache, so ten clusters is 20 GiB with nothing measuring
  the sum. The four are independent and each is worth its own commit. **Two questions to settle
  before writing any of it.** A fixed complexity limit only bounds a query's *shape*, because no
  field declares a cost — if fan-out is the real worry, the work is writing per-field cost functions
  and this stops being the cheap step. And a total disk budget needs a way back down: stopping a
  cache does not shrink it ([a stopped cache is held by its
  record](adr/2026-09-03-a-stopped-cache-is-held-by-its-record.md)), so a sweep that stops the
  largest each time ends with every cache stopped. Either it stops one and asks the user to clear
  it — a notice with a brake, and it should say so — or it evicts, which contradicts a standing ADR
  and needs a new one first.

- **R-08 — a release cannot be verified, and the pipeline making it is loose (medium; release owner).** Three gaps, all in `.github/`. `release-desktop.yml` grants `contents: write` and `deployments: write` at the top, so every job runs
  with the token that can publish; drop to `permissions: {}` and give only the final job what it
  needs (nothing creates a deployment, so that grant just goes). Audits run only on pull requests, so
  an advisory filed against a quiet `main` waits for whoever opens the next one — add a daily
  schedule. And the release job is gated on nothing: add a step that checks the required checks
  passed for the tag's commit: a release comes only from the tag push. Finally, write a short page on verifying a download — the GPG key,
  `shasum -c`, `codesign`/`spctl`, `signtool`, and plainly that Linux bundles are unsigned — and run
  it ourselves once before telling anyone else to. `.github/ci-config/zizmor.yml` ignores
  `release-desktop.yml`'s `excessive-permissions` finding until this lands; drop the ignore with the fix.

- **H-3 — three macOS entitlements ship unjustified (medium; desktop owner).**
  `src-tauri/entitlements.plist` grants `allow-jit`, `allow-unsigned-executable-memory` and
  `disable-library-validation` beside the uncontroversial network client, and the file's own comments
  admit nobody has checked whether they are needed. **This is an experiment, not a change.** Build
  with network access only, then walk the app: launch and paint every window on both CPU
  architectures, sync a cluster, scroll a table, sign in through the system browser, watch the
  sidecar spawn and exit cleanly, open a second window, reload. Add a grant back only when a crash
  report names the reason. **It must be a signed, notarized build** — ad-hoc local signing does not
  enforce the hardened runtime, so a local pass proves nothing, which is why this waits on R-08.
  Record what happened, including a clean pass, in a dated note under `docs/security/`, and give
  every surviving key one line saying what broke without it and on which macOS version. **Clear this
  blocker first:** every `release-desktop.yml` job checks out `refs/tags/desktop/v<version>`, so each variant
  means pushing a real tag until an optional `ref` input exists.

- **H-4a — the app never says it is out of date (medium; release owner).** Linux has no updater (H-4b)
  and no platform has a notice, so someone who installs a Linux package stays on that version until
  they happen to visit the releases page — including past a security fix. Saying so needs no signing
  key. Shape: the sidecar fetches `updates.kubetail.com`'s `latest-<key id>.json` (H-4b's manifest) on launch and
  every 12 hours by the wall clock, jittered, compares versions, and publishes the answer on the GraphQL surface; the
  webview shows a quiet notice whose link opens the release page in the system browser. The check
  belongs in the sidecar because the webview has no network access. It sends the version and nothing
  else — no identity, no machine id, no cluster data — and a failed check is silent, since not
  knowing about an update is where the user already was. It is **on by default**, said plainly in the
  settings dialog, with a toggle that persists in `host.json`: a notice nobody switches on is not a
  notice. Version comparison is a pure function and earns its own table-driven test, because an
  off-by-one here is a nag loop.

- **H-4b — the in-app updater is unchecked (medium; release owner).** The updater is built on macOS
  and Windows ([ADR](adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md),
  [record](security/2026-11-08-in-app-updater.md)); Linux applies no update. Publishing a final copies its
  manifests to `updates.kubetail.com` (`publish-desktop-updates.yml`). Two things remain. First,
  generate the current and the standby keys (`pnpm tauri signer generate`): the current private
  half and its password become the `production` environment's `TAURI_SIGNING_PRIVATE_KEY` and
  `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`, the standby's stays offline, apart from the current key's
  offline copy, and each public half and id are committed with its manifest's `latest-<id>.json`
  ([*Set up the keys*](updater-key.md#set-up-the-keys)). Then run the record's manual
  check once: an older build updates to a signed newer one published to a test release, and
  refuses one signed with a different key. Record the result there and update the security
  model's row.

- **Nothing exercises the release workflows' input validation (release owner).** `release-desktop.yml`
  takes a dispatched version only if `scripts/check-release-version.sh` accepts it, and
  the input reaches the config script as an environment value instead of as shell source. That is what
  keeps a crafted version string from running as shell in a job that holds release permissions and
  signing secrets. zizmor in `Lint · YAML` fails a `run:` block that interpolates the version, but the
  script has no test: the 4 September review ran eighteen cases by hand. **Fix:** feed the script
  a table of good and hostile strings — quotes, whitespace,
  `$(…)`, newlines, a build-metadata suffix — so the rule is exercised rather than remembered. **When
  it lands:** the *dispatched release version* row in [`security-model.md`](security-model.md) moves to
  **Enforced**.

- **Nobody has read the SBOM we publish (release owner).** Every release generates a CycloneDX SBOM
  (`anchore/sbom-action`). It reads the source tree, so whether it covers the native
  runtime libraries and the bundled sidecar binary is unknown. An SBOM that quietly omits half the
  bundle is worse than none, because it reads as complete. **Fix:** generate one, read it, and write
  down what it does and does not cover.

- **Nothing watches the parts we do not build (release/desktop owners).** The app runs on WebKitGTK,
  WebView2 and WKWebView, and ships binaries produced by the Go and Rust toolchains. `cargo audit` and
  `govulncheck` cover our dependencies, not these. A WebKitGTK advisory reaches a user through their
  distribution or not at all, and today we would not know either way. **Fix:** decide who watches
  what, and how a fix reaches a user. The macOS `minimumSystemVersion` in `tauri.conf.json` is the one
  lever we already have.

- **Dialling an insecure cluster is a choice nobody wrote down.** A context with
  `insecure-skip-tls-verify`, or a plain `http` server, is connected to like any other; the context bar
  badges it (`tlsUnverifiedReason`). Matching `kubectl` is deliberate — refusing would make the app
  useless on the clusters people actually run — but the model records only the badge, so connecting
  anyway reads as something nobody noticed. **Fix:** an ADR accepting it, and a **By decision** row
  beside the badge's row in [`security-model.md`](security-model.md).

- **The exec window arrives with an obligation.** It does not exist yet. When it does, what it
  displays is bytes from a cluster an attacker may control: no HTML interpretation, and terminal
  control sequences stripped before anything renders them. An exec stream has no such step yet, as
  a log line has (`services/logs`, and the viewer's row in [`security-model.md`](security-model.md)).
  **Fix:** the window brings its own test and its row.

## Testing

- **Coverage beyond lines.** Codecov gates line coverage of the changed lines (root `CLAUDE.md`). Not gated: branch coverage, per-file targets, and mutation testing. To try for mutation testing, report-only first: `cargo-mutants --in-diff`, Stryker's vitest runner, Gremlins for Go.
- **A local preview of the coverage gate.** The list of uncovered changed lines lives on the PR. If the push-and-read round trip proves slow, `diff-cover` over the same reports is the local preview to add.
- **Integration tests against the cloud model providers.** Every `internal/llm` test serves a synthetic body, so a vendor that changes its catalog shape, its finish reasons or what it accepts for `max_tokens` and `reasoning_effort` is found by a user, or by someone running `make llm-catalogs`, not by a test. Wanted: an opt-in suite behind a build tag, keyed from the environment, that runs each cloud row's discovery and one short turn per written effort against the real endpoint, kept out of `make test`.
- **Port what is left of the tests the LLM rewrite's purge removed.** The last step of the rewrite's sequence; its spec is not written yet.
- **Keep the no-wall-clock rule.** Both `CLAUDE.md` files state it, and the tree is currently clean: the frontend suites use `vi.useFakeTimers()` + `advanceTimersByTimeAsync`, `waitFor`, or the `flush()` helper with no `setTimeout` waits; Go's three `time.Sleep` calls are all the permitted kind and each says so — a widened truncate window in `kubeconfig_test.go`, a writer racing a gauge in `caches_test.go`, and `kubesync`'s deliberate exit latency; and `src-tauri/.../sidecar/ipc.rs`'s retry test — `#[tokio::test(start_paused = true)]`, letting tokio auto-advance virtual time between parked timers — is the shape to match. **What to watch for:** not `time.Sleep` but *thin real-timer margins* — tests that never sleep yet still fail on a loaded machine because they race short real durations. The fix shape is an injectable clock/timer seam so the test advances virtual time. The ~30 `time.After(...)` uses in sidecar tests are almost all *deadlines* guarding a channel receive, which the rule explicitly permits; keep those separate from any load-bearing wait. A `-count=20` soak on a loaded machine is the cheapest way to find regressions.

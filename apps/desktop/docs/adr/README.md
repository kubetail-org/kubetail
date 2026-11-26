# Kubetail Desktop ADRs

These ADRs record the desktop app's decisions: the webview, the Tauri host, the Go sidecar and the tooling and jobs only the app has. A decision that holds for the whole repo, the desktop included, goes in the repo's [`docs/adr/`](../../../../docs/adr/README.md), whose README sets the rules both sequences follow: filenames, frontmatter, sections, status and the exemption from "describe the present". [`TEMPLATE.md`](TEMPLATE.md) is this sequence's copy of the template.

What differs here:

- **`scope`** is one of `cross-cutting`, `frontend`, `host`, `sidecar`, `proto` or `repo`. Here `repo` means the desktop's own tooling and jobs: its signing, packaging, release files, platform splits and the security model.
- **Link by a path relative to the linking file**: `docs/adr/…` from `apps/desktop/CLAUDE.md`, `../docs/adr/…` from `apps/desktop/src/CLAUDE.md`.
- **A torn-out subsystem's ADRs are deleted, not superseded.** A teardown ahead of a rebuild has decided nothing yet, and a Superseded entry with no successor to point at is worse than none. Remove the files, their index rows and every inbound link in the same commit, and write the replacements as the new design lands.

## Index

| Date | ADR | Scope | Status |
| --- | --- | --- | --- |
| 2026-08-09 | [Route all webview GraphQL through Tauri IPC](2026-08-09-graphql-over-tauri-ipc.md) | cross-cutting | Accepted |
| 2026-08-09 | [Stream each kind as its own delta watch, joined client-side](2026-08-09-delta-watch-protocol.md) | cross-cutting | Accepted |
| 2026-08-09 | [Transport status keyed to the host's open frame](2026-08-09-transport-status-generation.md) | frontend | Accepted |
| 2026-08-30 | [Reduce subscription frames as a batch, once per animation frame](2026-08-30-batch-watch-frames-per-animation-frame.md) | frontend | Accepted |
| 2026-08-09 | [host.json as settings source of truth](2026-08-09-host-json-settings.md) | cross-cutting | Accepted |
| 2026-08-09 | [First-paint theming: inline script + native background](2026-08-09-first-paint-theming.md) | cross-cutting | Accepted |
| 2026-08-09 | [Per-platform window chrome](2026-08-09-per-platform-window-chrome.md) | host | Accepted |
| 2026-08-09 | [URL search params as window state](2026-08-09-url-params-as-window-state.md) | frontend | Accepted |
| 2026-08-09 | [Dashboard nav: curated tree merged with discovered kinds](2026-08-09-dashboard-nav-merge.md) | frontend | Accepted |
| 2026-08-09 | [Beehive owner chain with ObjectID identity](2026-08-09-beehive-control-plane.md) | sidecar | Accepted |
| 2026-08-09 | [Every condition is a liveness condition](2026-08-09-liveness-conditions.md) | sidecar | Accepted |
| 2026-08-09 | [Local-first auth; settings sync depends on auth](2026-08-09-local-first-auth-settings.md) | sidecar | Accepted |
| 2026-08-09 | [Resync via fan-out poke, not cascade](2026-08-09-poke-resync-fanout.md) | cross-cutting | Accepted |
| 2026-08-14 | [Report a dead watch as a terminal GraphQL error](2026-08-14-watch-failure-reporting.md) | cross-cutting | Accepted |
| 2026-08-16 | [Compose start/stop/close as one shape](2026-08-16-lifecycle-composition.md) | sidecar | Accepted |
| 2026-08-18 | [Run cluster discovery as a beehive kind](2026-08-18-discovery-as-a-beehive-kind.md) | sidecar | Accepted |
| 2026-08-22 | [Address every Kubernetes connection by ClusterID](2026-08-22-connections-addressed-by-cluster-id.md) | sidecar | Accepted |
| 2026-08-23 | [Wake cluster passes from a fleet-wide kubeconn bus](2026-08-23-kubeconn-wakes-ride-a-fleet-bus.md) | sidecar | Accepted |
| 2026-08-23 | [One connection per kube-context](2026-08-23-one-connection-per-context.md) | sidecar | Accepted |
| 2026-08-24 | [Extract the probe scheduler into a Kubernetes-free engine](2026-08-24-probe-engine.md) | sidecar | Accepted |
| 2026-08-25 | [The connection probe dials /api, builds the connection, and lets the pool retire it](2026-08-25-connection-probe-dial.md) | sidecar | Accepted |
| 2026-08-25 | [A connection carries the identity confirmed over it](2026-08-25-connection-carried-identity.md) | sidecar | Accepted |
| 2026-08-26 | [One SQLite file per cache, behind a refcounted registry](2026-08-26-cache-store-per-cache.md) | sidecar | Accepted |
| 2026-08-26 | [The cache store signals with a coalesced ping, not a row delta](2026-08-26-store-change-ping-bus.md) | sidecar | Accepted |
| 2026-08-26 | [Cached-data watches ping, re-read and diff, and a cleared cache ends one cleanly](2026-08-26-cached-data-read-loop.md) | sidecar | Accepted |
| 2026-08-27 | [A recorded identity conflict rebuilds the connection, woken by an edge](2026-08-27-identity-driven-retirement.md) | sidecar | Accepted |
| 2026-08-28 | [The probe engine is a supervisor, and a probe is a reconciler](2026-08-28-supervisor-vocabulary.md) | sidecar | Accepted |
| 2026-08-28 | [The supervisor runs two kinds of thing — jobs and workers](2026-08-28-jobs-and-workers.md) | sidecar | Accepted |
| 2026-08-28 | [A cache's sync is armed by a record's pass, never by a reader](2026-08-28-arming-is-policy-never-interest.md) | sidecar | Accepted |
| 2026-08-28 | [A record anchors a timeline; it does not mirror a status](2026-08-28-records-as-timeline-anchors.md) | sidecar | Accepted |
| 2026-08-29 | [The objects watch reads identity, and fetches a body only for the rows it sends](2026-08-29-object-read-split.md) | sidecar | Accepted |
| 2026-08-30 | [Redact credentials on the way into the cache, and never store a function of a secret](2026-08-30-secret-redaction-at-write-time.md) | sidecar | Accepted |
| 2026-08-30 | [Resolve a connection retry when its own probe finishes](2026-08-30-retry-resolves-with-its-probe.md) | sidecar | Accepted |
| 2026-08-30 | [Stamp every row with its write position, and log deletes](2026-08-30-write-positions-and-the-deletes-log.md) | sidecar | Accepted |
| 2026-09-02 | [Kind records mirror the catalog on disk, and Paused is the user's field](2026-09-02-kind-records-mirror-the-catalog.md) | sidecar | Accepted |
| 2026-09-02 | [The cache store prepares every statement once and binds collections as JSON](2026-09-02-kubestore-sql-discipline.md) | sidecar | Accepted |
| 2026-09-02 | [One janitor per open cache file, gated on the freelist and trimming per kind](2026-09-02-kubestore-janitor.md) | sidecar | Accepted |
| 2026-09-02 | [The Cluster pass folds its claim into two conditions and never writes timing](2026-09-02-cluster-conditions-two-subjects.md) | sidecar | Accepted |
| 2026-09-02 | [Every non-watch request carries an idle-read bound](2026-09-02-idle-read-bound.md) | sidecar | Accepted |
| 2026-09-02 | [The discovery sweep writes only on a changed fingerprint, drops a fixed list, and never prunes a partial answer](2026-09-02-discovery-sweep-rules.md) | sidecar | Accepted |
| 2026-09-02 | [A kind sync proves itself by a frame, resumes off its cookie, and is judged at read time](2026-09-02-kind-sync-verdicts.md) | sidecar | Accepted |
| 2026-09-02 | [Cache health is a read-side fold over the records, with paused kinds resolved first](2026-09-02-cache-health-fold.md) | sidecar | Accepted |
| 2026-09-02 | [Three event timelines, written unconditionally and read by id alone](2026-09-02-event-timelines.md) | sidecar | Accepted |
| 2026-09-02 | [Let the profile ACL protect the cache files on Windows](2026-09-02-windows-cache-files-rely-on-the-profile-acl.md) | cross-cutting | Accepted |
| 2026-09-02 | [The cluster cache is ordinary application data, not credential-bearing storage](2026-09-02-the-cache-is-ordinary-application-data.md) | sidecar | Accepted |
| 2026-09-03 | [Status mirrors the kubeconfig; the frontend draws the verdicts](2026-09-03-status-mirrors-the-kubeconfig.md) | cross-cutting | Accepted |
| 2026-09-03 | [Bound the cache by total size, not by per-table event retention](2026-09-03-bound-the-cache-by-total-size.md) | sidecar | Accepted |
| 2026-09-03 | [A cache stopped at its size ceiling is held there by its own record](2026-09-03-a-stopped-cache-is-held-by-its-record.md) | sidecar | Accepted |
| 2026-09-03 | [No GraphQL operation allowlist; the shipped set converges on the schema](2026-09-03-no-graphql-operation-allowlist.md) | cross-cutting | Accepted |
| 2026-09-03 | [The schema's breadth is held by review; the capability file is pinned](2026-09-03-schema-breadth-is-held-by-review.md) | cross-cutting | Accepted |
| 2026-09-05 | [Track the advisories Tauri's dependency graph pins](2026-09-05-tauri-pinned-transitive-advisories.md) | host | Accepted |
| 2026-09-07 | [Import the login shell's environment through an allowlist, and leave a `~` verbatim](2026-09-07-shell-environment-allowlist.md) | sidecar | Accepted |
| 2026-09-08 | [Two processes, two log files](2026-09-08-two-processes-two-log-files.md) | cross-cutting | Accepted |
| 2026-09-08 | [JSON in both log files, text rendered by the host](2026-09-08-json-logs-rendered-by-the-host.md) | cross-cutting | Accepted |
| 2026-09-08 | [Webview records get their own log file](2026-09-08-webview-records-get-their-own-log-file.md) | host | Accepted |
| 2026-09-10 | [Keep chats in app.db](2026-09-10-chats-live-in-app-db.md) | sidecar | Accepted |
| 2026-09-10 | [The answer is a record, not a stream the caller owns](2026-09-10-the-answer-is-a-record.md) | sidecar | Accepted |
| 2026-09-11 | [The record is the app's blocks, not one provider's wire format](2026-09-11-the-record-is-the-apps-not-the-providers.md) | sidecar | Accepted |
| 2026-09-11 | [The cluster is the app's scope](2026-09-11-the-cluster-is-the-apps-scope.md) | frontend, sidecar | Accepted |
| 2026-09-13 | [The cluster card rides the user message](2026-09-13-the-cluster-card-rides-the-user-message.md) | sidecar | Accepted |
| 2026-09-14 | [The cluster card carries no counts](2026-09-14-the-cluster-card-carries-no-counts.md) | sidecar | Accepted |
| 2026-09-14 | [app.db prepares every statement once and reads on its own pool](2026-09-14-app-db-sql-discipline.md) | sidecar | Accepted |
| 2026-09-14 | [Tool rounds live in the answer's row](2026-09-14-tool-rounds-live-in-the-answers-row.md) | sidecar | Accepted |
| 2026-09-16 | [The app owns app.db and hands it to its services](2026-09-16-the-app-owns-app-db.md) | sidecar | Accepted |
| 2026-09-16 | [One statement set prepares and routes every store's statements](2026-09-16-sqlstmt-prepares-a-services-statements.md) | sidecar | Accepted |
| 2026-09-16 | [A cluster is a row in app.db, mirrored into a runtime object](2026-09-16-clusters-are-rows-mirrored-into-beehive.md) | cross-cutting | Accepted |
| 2026-09-17 | [A turn is a run over its message, filed in one transaction](2026-09-17-a-turn-is-a-run-over-its-message.md) | cross-cutting | Accepted |
| 2026-09-17 | [A tool call is a committed row before it runs](2026-09-17-a-tool-call-is-a-committed-row-before-it-runs.md) | sidecar | Accepted |
| 2026-09-20 | [The wire a chat is pinned to is an LLM dialect](2026-09-20-the-pin-is-an-llm-dialect.md) | cross-cutting | Accepted |
| 2026-09-21 | [The llm package wires itself, in one package](2026-09-21-llm-is-one-package.md) | sidecar | Accepted |
| 2026-09-22 | [The gate is the tool's card, and gated calls run one at a time](2026-09-22-the-gate-is-the-tools-card.md) | sidecar | Accepted |
| 2026-09-23 | [A tool call shows itself from its arguments, and an approval is only the decision](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md) | cross-cutting | Accepted |
| 2026-09-24 | [Every tool the model can call is in one box, and what it is follows from what it implements](2026-09-24-every-tool-is-in-the-box.md) | sidecar | Accepted |
| 2026-09-24 | [The catalog is its own package, and lists every tool each provider is offered](2026-09-24-the-catalog-is-its-own-package.md) | sidecar | Accepted |
| 2026-09-24 | [Memory is rows, one fact each, scoped to a cluster or every cluster](2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md) | cross-cutting | Accepted |
| 2026-09-25 | [An agent is a tool in the box, gated by its calls rather than an allowlist](2026-09-25-an-agent-is-a-tool-in-the-box.md) | sidecar | Accepted |
| 2026-09-25 | [An agent runs in the background, as a task of the chat that owns its rows](2026-09-25-an-agent-runs-in-the-background.md) | cross-cutting | Accepted |
| 2026-09-25 | [A chat can switch to any model, and each run records the dialect it ran on](2026-09-25-a-chat-can-switch-to-any-model.md) | cross-cutting | Accepted |
| 2026-09-27 | [The cluster is kubectl in Bash, and KubeQuery over the mirror](2026-09-27-the-cluster-is-kubectl-in-bash-and-kubequery.md) | sidecar | Accepted |
| 2026-09-27 | [KubeQuery reads views on a connection of its own](2026-09-27-kubequery-reads-views-on-a-connection-of-its-own.md) | sidecar | Accepted |
| 2026-09-27 | [KubeQuery reads without asking](2026-09-27-kubequery-reads-without-asking.md) | sidecar | Accepted |
| 2026-09-27 | [KubeQuery reads tables written with the object](2026-09-27-kubequery-reads-tables-written-with-the-object.md) | sidecar | Accepted |
| 2026-09-27 | [The cluster service resolves a cluster](2026-09-27-the-cluster-service-resolves-a-cluster.md) | sidecar | Accepted |
| 2026-09-27 | [A tool calls its service directly](2026-09-27-a-tool-calls-its-service-directly.md) | sidecar | Accepted |
| 2026-09-27 | [A quiet watch is reopened from its cookie, and reads Stale only when the reopen hangs](2026-09-27-a-quiet-watch-is-reopened-from-its-cookie.md) | sidecar | Accepted |
| 2026-09-27 | [Keep each file in the platform's directory for its kind](2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md) | cross-cutting | Accepted |
| 2026-09-28 | [Ship Kubetail's own bwrap, with Ubuntu's profile for it](2026-09-28-kubetail-ships-its-own-bwrap.md) | cross-cutting | Accepted |
| 2026-09-28 | [Keep a macOS run's processes in its group by refusing setsid and setpgid](2026-09-28-a-macos-run-keeps-its-group-by-refusing-setsid.md) | sidecar | Accepted |
| 2026-09-28 | [The sandbox is the gate for a sandboxed command](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md) | cross-cutting | Accepted |
| 2026-09-28 | [Helm's release Secrets pass, redacted inside](2026-09-28-helm-release-secrets-pass-redacted-inside.md) | sidecar | Accepted |
| 2026-09-28 | [A sandboxed forwarder holds a loopback port on macOS](2026-09-28-a-sandboxed-forwarder-holds-a-loopback-port-on-macos.md) | sidecar | Accepted |
| 2026-09-28 | [Offer no sandbox on native Windows; WSL2 runs the Linux one](2026-09-28-native-windows-has-no-sandbox.md) | sidecar | Accepted |
| 2026-09-29 | [A sandboxed command asks for each cluster write](2026-09-29-a-sandboxed-command-asks-for-each-cluster-write.md) | cross-cutting | Accepted |
| 2026-09-29 | [A quiet reopen the server refuses as expired relists in place](2026-09-29-an-expired-quiet-reopen-relists-in-place.md) | sidecar | Accepted |
| 2026-09-30 | [The cluster service is one package of record families over private leaves](2026-09-30-the-cluster-service-is-one-package-over-private-leaves.md) | sidecar | Accepted |
| 2026-09-30 | [A supervisor run may bring its next run forward, never push it back](2026-09-30-a-run-may-bring-its-next-run-forward.md) | sidecar | Accepted |
| 2026-09-30 | [The kubeconfig fingerprint digests the whole file](2026-09-30-the-kubeconfig-fingerprint-digests-the-whole-file.md) | sidecar | Accepted |
| 2026-09-30 | [Leaving the sandbox is the user's switch for a chat, never the model's flag](2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md) | cross-cutting | Accepted |
| 2026-10-02 | [The sandbox's zones are Kubetail's lists, and its environment one table](2026-10-02-the-sandboxs-zones-are-kubetails-lists.md) | sidecar | Accepted |
| 2026-10-02 | [Bound a sandboxed run's processes with resource limits set inside the run](2026-10-02-process-limits-are-set-inside-the-run.md) | sidecar | Accepted |
| 2026-10-03 | [Authorization is binary, and a prompt is a denial the user may lift](2026-10-03-authorization-is-binary-and-a-prompt-is-a-denial-the-user-may-lift.md) | sidecar | Accepted |
| 2026-10-04 | [Run the login shell in the sandbox](2026-10-04-the-login-shell-runs-in-the-sandbox.md) | sidecar | Accepted |
| 2026-10-04 | [A prompt names the action and offers a duration](2026-10-04-a-prompt-names-the-action-and-offers-a-duration.md) | cross-cutting | Accepted |
| 2026-10-04 | [Network is the user's switch](2026-10-04-network-is-the-users-switch.md) | cross-cutting | Accepted |
| 2026-10-04 | [Ship Kubetail's own pasta, with passt's profile for it](2026-10-04-kubetail-ships-its-own-pasta.md) | cross-cutting | Accepted |
| 2026-10-04 | [A folder grant is a rule, and the file tools walk it by handle](2026-10-04-a-folder-grant-is-a-rule.md) | cross-cutting | Accepted |
| 2026-10-05 | [Secret data is a permissioned read](2026-10-05-secret-data-is-a-permissioned-read.md) | cross-cutting | Accepted |
| 2026-10-05 | [A monitor run is a run of the chat service](2026-10-05-a-monitor-run-is-a-run-of-the-chat-service.md) | sidecar | Accepted |
| 2026-10-06 | [Group the sidecar's packages into services, lib and the rest](2026-10-06-sidecar-packages-group-into-services-lib-and-the-rest.md) | sidecar | Accepted |
| 2026-10-06 | [Remove cloud settings sync until settings have a design](2026-10-06-remove-cloud-settings-sync.md) | sidecar | Accepted |
| 2026-10-06 | [Build every service into one runtime, in order, and never hand it to a service](2026-10-06-the-runtime-is-built-in-order.md) | sidecar | Accepted |
| 2026-10-07 | [Group the sidecar's packages into layers](2026-10-07-sidecar-folders-are-layers.md) | sidecar | Accepted |
| 2026-10-08 | [A tool whose action is what its run showed records it at the run](2026-10-08-a-shown-tool-records-its-action-at-the-run.md) | cross-cutting | Accepted |
| 2026-10-13 | [A macOS run leads its own session, so no run reads another process's launch strings](2026-10-13-a-macos-run-leads-its-own-session-so-no-run-reads-another-processs-launch-strings.md) | sidecar | Accepted |
| 2026-10-13 | [The sidecar's own mirror reads keep their connections](2026-10-13-the-sidecars-own-mirror-reads-keep-their-connections.md) | sidecar | Accepted |
| 2026-10-13 | [A cache reading is one snapshot](2026-10-13-a-cache-reading-is-one-snapshot.md) | sidecar | Accepted |
| 2026-10-17 | [Keep a cluster cache's log lines in a file of its own, evicted by read](2026-10-17-the-log-cache.md) | sidecar | Accepted |
| 2026-10-17 | [A head read never links, and a line carries the hole before it](2026-10-17-a-head-read-never-links.md) | sidecar | Accepted |
| 2026-10-17 | [Record a bash stop as the runner's act, and kill at once on a cancel](2026-10-17-a-bash-stop-is-the-runners-record.md) | sidecar | Accepted |
| 2026-10-17 | [A discovered model is listed only on its catalog's word](2026-10-17-a-discovered-model-is-listed-on-its-catalogs-word.md) | sidecar | Accepted |
| 2026-10-17 | [Bound a model stream by wire silence, per dialect](2026-10-17-a-model-stream-is-bounded-by-wire-silence.md) | sidecar | Accepted |
| 2026-10-17 | [Reclaim a gone sidecar's run directories by its lock, not its pid](2026-10-17-a-run-directory-is-reclaimed-by-its-sidecars-lock.md) | sidecar | Accepted |
| 2026-10-17 | [Give a sandboxed run a kubeconfig whose server is fixed and whose proxy holds the token](2026-10-17-a-sandboxed-kubeconfig-names-a-fixed-server.md) | sidecar | Accepted |
| 2026-10-17 | [Refuse a send the chat cannot fit, from the provider's own count](2026-10-17-a-send-the-chat-cannot-fit-is-refused-from-the-providers-count.md) | sidecar | Accepted |
| 2026-10-17 | [Ollama is local, and OLLAMA_HOST says where](2026-10-17-ollama-is-local-and-ollama-host-says-where.md) | sidecar | Accepted |
| 2026-10-17 | [The cluster proxy reads a request as the API server will](2026-10-17-the-cluster-proxy-reads-a-request-as-the-api-server-does.md) | sidecar | Accepted |
| 2026-10-17 | [Read the login shell's answer by markers, from a session of its own](2026-10-17-the-login-shell-is-read-by-markers-in-a-session-of-its-own.md) | sidecar | Accepted |
| 2026-10-17 | [Mark the prompt cache for an hour, and only where a provider needs marks](2026-10-17-the-messages-wire-caches-the-prompt-for-an-hour.md) | sidecar | Accepted |
| 2026-10-17 | [Split tests by platform file, take seams as parameters, and never sleep](2026-10-17-tests-split-by-platform-take-seams-as-parameters-and-never-sleep.md) | cross-cutting | Accepted |
| 2026-10-17 | [Let the virtual table correct its own scroll position](2026-10-17-the-virtual-table-owns-its-scroll-anchoring.md) | frontend | Accepted |
| 2026-10-17 | [Keep the chrome's shape as an app-wide default in localStorage](2026-10-17-chrome-shape-is-an-app-wide-default-in-localstorage.md) | frontend | Accepted |
| 2026-10-17 | [Hold each chat's draft and send in an outbox above the routes](2026-10-17-the-chat-outbox-holds-each-chats-send-above-the-routes.md) | frontend | Accepted |
| 2026-10-18 | [Backward log reads assume normal logs, and stop clearly where normal ends](2026-10-18-backward-reads-assume-normal-logs.md) | sidecar | Accepted |
| 2026-10-18 | [A rich view is a tool call's action](2026-10-18-a-rich-view-is-a-tool-calls-action.md) | cross-cutting | Accepted |
| 2026-10-23 | [A log read is a window over a range](2026-10-23-a-log-read-is-a-window-over-a-range.md) | cross-cutting | Accepted |
| 2026-10-24 | [A bounded log window reads its dropped lines back from a moment](2026-10-24-a-bounded-log-window-reads-dropped-lines-back.md) | cross-cutting | Accepted |
| 2026-10-25 | [A tool call records its tool's contract beside its stored JSON](2026-10-25-a-tool-call-records-its-tools-contract.md) | sidecar | Accepted |
| 2026-10-25 | [The sidecar's own mirror statements compile once per file](2026-10-25-the-sidecars-own-mirror-statements-compile-once-per-file.md) | sidecar | Accepted |
| 2026-10-26 | [Serve GraphQL and gRPC over one socket with the standard library's h2c](2026-10-26-single-socket-h2c-from-the-standard-library.md) | cross-cutting | Accepted |
| 2026-10-28 | [Tauri signs the macOS bundle in the job that installs its dependencies](2026-10-28-tauri-signs-the-macos-bundle-beside-its-dependencies.md) | repo | Accepted |
| 2026-10-29 | [Each persisted format carries its own version stamp, and a downgrade is refused only where it would break](2026-10-29-each-persisted-format-carries-its-own-version-stamp.md) | cross-cutting | Accepted |
| 2026-10-29 | [The omnibox is the cluster picker](2026-10-29-the-omnibox-is-the-cluster-picker.md) | frontend | Accepted |
| 2026-10-30 | [TypeScript coverage is measured without the React compiler](2026-10-30-typescript-coverage-is-measured-without-the-react-compiler.md) | frontend | Accepted |
| 2026-11-02 | [A series is a third kind of stream, and the pool owns its clock](2026-11-02-a-series-is-a-third-kind-of-stream-and-the-pool-owns-its-clock.md) | cross-cutting | Accepted |
| 2026-11-03 | [Connected carries the probe's class of failure as its reason](2026-11-03-connected-carries-the-probes-class-of-failure.md) | cross-cutting | Accepted |
| 2026-11-03 | [A failed kind sync carries its class of failure beside its reason](2026-11-03-a-failed-sync-carries-its-class-of-failure.md) | cross-cutting | Accepted |
| 2026-11-03 | [A kind reads Stale only when its watch stops sending bookmarks](2026-11-03-a-kind-reads-stale-only-when-its-bookmarks-stop.md) | sidecar | Accepted |
| 2026-11-03 | [A Connecting cache answers KubeQuery from the kinds that have listed](2026-11-03-a-connecting-cache-answers-from-the-kinds-that-have-listed.md) | sidecar | Accepted |
| 2026-11-04 | [A log window belongs to one app window, and the host keeps the pair](2026-11-04-a-log-window-belongs-to-one-app-window-and-the-host-keeps-the-pair.md) | cross-cutting | Accepted |
| 2026-11-05 | [Lint the webview with oxlint's categories and a chosen rule list](2026-11-05-lint-the-webview-with-oxlints-categories-and-a-chosen-rule-list.md) | frontend | Accepted |
| 2026-11-05 | [Lint the React compiler's rules with oxlint's native ports](2026-11-05-lint-the-react-compilers-rules-with-oxlints-native-ports.md) | frontend | Accepted |
| 2026-11-06 | [Desktop pre-releases are rc.N, mapped to a version per package format](2026-11-06-desktop-pre-releases-are-rc-n-mapped-per-package-format.md) | repo | Accepted |
| 2026-11-07 | [Linux ships only the .deb and the .rpm](2026-11-07-linux-ships-only-the-deb-and-the-rpm.md) | repo | Accepted |
| 2026-11-08 | [Release files are named kubetail-\<product\>-\<version\>-\<os\>-\<arch\>](2026-11-08-release-files-are-named-kubetail-product-version-os-arch.md) | repo | Accepted |
| 2026-11-08 | [A shipped schema changes only by a new migration](2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md) | cross-cutting | Accepted |
| 2026-11-08 | [The updater trusts a current and a standby key](2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md) | cross-cutting | Accepted |
| 2026-11-10 | [CI checks every platform split the code has on a pull request](2026-11-10-ci-tests-every-platform-split-the-code-has.md) | repo | Accepted |
| 2026-11-10 | [Main builds and tests the desktop host's release profile on the release runners](2026-11-10-main-builds-and-tests-the-release-profile.md) | repo | Accepted |
| 2026-11-12 | [A model may override its provider's default dialect](2026-11-12-a-model-may-override-its-providers-default-dialect.md) | sidecar | Accepted |
| 2026-11-12 | [The dashboard nav groups kinds by scope and API group, and lists every kind flat](2026-11-12-the-dashboard-nav-groups-kinds-by-scope-and-api-group.md) | frontend | Accepted |
| 2026-11-12 | [The dashboard's namespace scope is a search param](2026-11-12-the-namespace-scope-is-a-search-param.md) | frontend | Accepted |
| 2026-11-15 | [designsync reads Figma over the REST API](2026-11-15-designsync-reads-figma-over-the-rest-api.md) | repo | Accepted |
| 2026-11-17 | [A sandboxed command reaches a cloud platform through a proxy on its run's one relay](2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md) | sidecar | Accepted |
| 2026-11-17 | [A sandboxed command reaches AWS through a re-signing proxy](2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md) | sidecar | Accepted |
| 2026-11-17 | [A sandboxed command reaches Azure through a token-injecting proxy](2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md) | sidecar | Accepted |
| 2026-11-17 | [A sandboxed command reaches GitHub over plain HTTP at gh's own host name](2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md) | sidecar | Accepted |
| 2026-11-17 | [Google Cloud is reached with the active gcloud account's token](2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md) | sidecar | Accepted |
| 2026-11-18 | [Every cloud platform is on until the user turns it off](2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md) | sidecar | Accepted |
| 2026-11-18 | [Accept picomatch's ReDoS advisory in dev tooling](2026-11-18-accept-picomatchs-redos-advisory-in-dev-tooling.md) | repo | Accepted |
| 2026-11-18 | [A run's AWS identity follows its chat's cluster](2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md) | sidecar | Accepted |
| 2026-11-18 | [The ChatGPT connector signs in with ChatGPT's dynamic registration](2026-11-18-the-chatgpt-connector-signs-in-with-chatgpts-dynamic-registration.md) | cross-cutting | Accepted |
| 2026-11-18 | [An OAuth connection is a row in app.db matched to its keyring entry](2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md) | sidecar | Accepted |
| 2026-11-18 | [A provider's credential is a key or a signer](2026-11-18-a-providers-credential-is-a-key-or-a-signer.md) | sidecar | Accepted |
| 2026-11-18 | [A signed-in provider's catalog follows its connection](2026-11-18-a-signed-in-providers-catalog-follows-its-connection.md) | sidecar | Accepted |
| 2026-11-19 | [A connector is an OAuth session or a granted key](2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md) | sidecar | Accepted |
| 2026-11-19 | [A connector is one package, and its machinery is internal](2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md) | cross-cutting | Accepted |
| 2026-11-19 | [The Kubetail sign-in is a connector on the OAuth engine](2026-11-19-the-kubetail-sign-in-is-a-connector-on-the-oauth-engine.md) | sidecar | Accepted |
| 2026-11-19 | [The AWS connector is AWS login with a DPoP-bound session](2026-11-19-the-aws-connector-is-aws-login-with-a-dpop-bound-session.md) | sidecar | Accepted |
| 2026-11-19 | [The GitHub connector signs in with GitHub's device flow](2026-11-19-the-github-connector-signs-in-with-githubs-device-flow.md) | sidecar | Accepted |
| 2026-11-19 | [The Google connector signs Vertex AI in the user's project](2026-11-19-the-google-connector-signs-vertex-ai-in-the-users-project.md) | sidecar | Accepted |
| 2026-11-20 | [A provider's credential follows one order](2026-11-20-a-providers-credential-follows-one-order.md) | cross-cutting | Accepted |
| 2026-11-20 | [Keep a setting with a cluster scope as rows in app.db](2026-11-20-a-setting-with-scopes-is-rows.md) | sidecar | Accepted |
| 2026-11-20 | [The user's variables reach commands on either side of the sandbox](2026-11-20-the-users-variables-reach-commands-on-either-side-of-the-sandbox.md) | sidecar, webview | Accepted |
| 2026-11-20 | [Every provider is one row, signed by one service](2026-11-20-every-provider-is-one-row-signed-by-one-service.md) | cross-cutting | Accepted |
| 2026-11-20 | [A launch restores keyring credentials in the background](2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md) | cross-cutting | Accepted |
| 2026-11-21 | [Keep the tool settings as rows in app.db](2026-11-21-tool-settings-are-rows-in-app-db.md) | sidecar | Accepted |
| 2026-11-21 | [Keep host.json in the Kubetail config directory](2026-11-21-host-json-lives-under-the-kubetail-config-directory.md) | host | Accepted |
| 2026-11-21 | [Linux folders follow XDG](2026-11-21-linux-folders-follow-xdg.md) | host | Accepted |
| 2026-11-22 | [Paths are compared as files](2026-11-22-paths-are-compared-as-files.md) | sidecar | Accepted |
| 2026-11-22 | [Each folder kind has its own folder on every platform](2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md) | host | Accepted |
| 2026-11-23 | [Treat two mounts of one folder as one PATH folder](2026-11-23-two-mounts-of-one-folder-are-one-path-folder.md) | sidecar | Accepted |
| 2026-11-23 | [The sidecar places Kubetail's folders itself](2026-11-23-the-sidecar-places-kubetails-folders-itself.md) | cross-cutting | Accepted |
| 2026-11-24 | [Follow a link to a Kubetail folder once, at start, and refuse one below it](2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md) | cross-cutting | Accepted |
| 2026-11-24 | [A folder grant is its own row, and a mode and a rule belong to a cluster](2026-11-24-a-folder-grant-is-its-own-row-and-a-mode-and-a-rule-belong-to-a-cluster.md) | sidecar | Accepted |
| 2026-11-24 | [A sandboxed command's PATH is one setting the user writes](2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md) | sidecar, webview | Accepted |
| 2026-11-24 | [Keep every setting as rows in app.db](2026-11-24-settings-are-rows-in-app-db.md) | sidecar | Accepted |
| 2026-11-25 | [The security model is a file per subject, checked against the code](2026-11-25-the-security-model-is-a-file-per-subject-checked-against-the-code.md) | repo | Accepted |
| 2026-11-25 | [Decide every target's request by access level, a boundary, a mode and rules over one action](2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md) | cross-cutting | Accepted |
| 2026-11-25 | [Name every data access for its family, kind and operation](2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md) | cross-cutting | Accepted |
| 2026-11-25 | [A window fixes its cluster, and Kubetail never falls back from an unreachable one](2026-11-25-a-window-fixes-its-cluster-and-never-falls-back.md) | cross-cutting | Accepted |
| 2026-11-25 | [Import Kstack without its history and reset its migrations](2026-11-25-import-kstack-without-its-history-and-reset-its-migrations.md) | repo | Accepted |
| 2026-11-26 | [Call a query or a subscription a read, and its contract a read contract](2026-11-26-call-a-query-or-a-subscription-a-read-and-its-contract-a-read-contract.md) | cross-cutting | Accepted |
| 2026-11-26 | [Write the read contracts once and each surface's rules beside them](2026-11-26-write-the-read-contracts-once-and-each-surfaces-rules-beside-them.md) | cross-cutting | Accepted |

# Security record — monitoring is on by default, 27 November 2026

**Subject:** the monitor's switch moves from `clusters.monitoring_enabled`, off by default, to
`monitor_targets.enabled`, on by default, and gates detection. The living model is
[security-model.md](../security-model.md); the monitor run this switch once gated is
[the monitoring session](2026-10-05-the-monitoring-session.md).

## What changes

Every cluster gets a monitor target, enabled, when the monitor's reconcile first lists it. An
enabled target is read from its cluster's cache every 30 seconds: a fixed list of statements
over the cache the cluster service already keeps. Detection makes no model call, no request to
the cluster and no network request, so turning it on by default spends nothing.

The monitoring session record describes a run as gated by the switch. From this change the
switch is on for every cluster, so that gate no longer keeps a run from starting.

## What holds

- **A run still checks the switch in the transaction that inserts it.** `chat.RunMonitor` takes
  an admit function from its caller and calls it inside the run's insert, with the run's id; a
  refusal is `ErrMonitoringOff` and writes nothing (`TestRunMonitorAdmitsInsideTheInsert`,
  `TestRunMonitorRefuses`). `chat` reads no table of the monitor's: the check is the caller's.
- **No run starts unasked.** `RunMonitor` has no caller until step 4 of the desktop monitoring
  work, whose one caller is the user's press of *Triage with AI*; step 5 gates unattended runs on
  the automatic-triage setting, which is off by default.
- **Detection reads only the cache.** The Kubernetes integration's statements run through
  `CachedData().ReadActive`, read-only, on one snapshot of the active cache, every table named
  `main.` (`TestEachKindReadSearchesItsIndex`). What they find is stored bounded, as text
  (`TestALongOrControlNameIsCutToFit`, `TestAnObservationOverABoundMakesItsSignalInvalid`).

## Residual risk

None new. A cluster's names, reasons and messages reach `app.db` through the incidents, as they
already reach it through the cache; the webview draws them through `VisibleText` when step 3a
shows them.

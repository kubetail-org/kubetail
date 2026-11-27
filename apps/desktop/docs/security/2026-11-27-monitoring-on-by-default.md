# Security record — monitoring on by default, 27 November 2026

**Subject:** `clusters.monitoring_enabled`, which now defaults on, and the monitor
(`sidecar/internal/agent/monitor`) it gates. The living model is
[security-model.md](../security-model.md).

## What it changes

The switch gates detection alone. The monitor reads each monitored cluster's cache with the
sidecar's own SQL, folds what it finds into incidents and stores them in `app.db`. It calls no
model and reaches no network. A row the user has not touched is monitored.

[The monitoring session record](2026-10-05-the-monitoring-session.md) describes a monitor run as
gated by this switch. `chat.RunMonitor` keeps that check, but every cluster now passes it by
default.

## What holds

- **Nothing starts a model run unasked.** `chat.RunMonitor` has no caller outside its tests: the
  monitor does not import it. Its first caller, the user's press on an incident, comes with triage;
  unattended runs come later, gated on a triage setting that is off by default.
- **Detection reads only the cache.** The monitor's statements run through
  `CachedData().ReadActive`, on the read-only own-query connection, bounded at 50,000 rows and
  16 MiB each (`TestACutAnswerLeavesItsSignalsUnjudged`).
- **Cluster text is stored, never interpreted.** A subject's name is bound as a parameter, of
  any length and any bytes (`TestASubjectNameOfAnyTextIsStored`).

## Residual risk

None new: the incidents hold names already in the cache file, which holds cluster data in the clear
by decision. `security-model/cluster-proxy.md` gains no row, since no protection moves; KUBE-41, what a monitor run refuses, links this record.

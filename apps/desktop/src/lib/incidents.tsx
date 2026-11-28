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

// What monitoring found: every target, every target's incidents and the signals they
// name, held once for the window so the bell and the Incidents dialog read the same
// maps, the occurrence feed of one incident, and the switch on a target. See
// docs/read-contracts/README.md, *Collection watch*, *List* and *Event feed*. The host
// shows each alert as an OS notification: this module reads whether the OS allows
// one, and opens the dialog on the incident a notification's press names.
//
// The webview knows an integration in one place, `INTEGRATION_LABEL`: a new value of
// `MonitorIntegration` fails the build here and nowhere else.
import { Channel, invoke } from '@tauri-apps/api/core';
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery } from 'urql';

import { graphql } from '@/gql';
import type {
  IncidentCollectionWatchSubscription,
  IncidentOccurrenceEndReason,
  IncidentOccurrenceEventFeedSubscription,
  MonitorIntegration,
  MonitorReadError,
  MonitorSeverity,
  MonitorSignalListQuery,
  MonitorTargetCollectionWatchSubscription,
} from '@/gql/graphql';
import type { Tone } from '@/lib/cluster-health';
import { useClusters } from '@/lib/clusters';
import { useDialogHost } from '@/lib/dialog';
import { reportHostError } from '@/lib/error-bus';
import { collectionWatchReducer } from '@/lib/graphql/collection-watch';
import { eventFeedReducer } from '@/lib/graphql/event-feed';
import type { EventFeedHistory } from '@/lib/graphql/event-feed';
import { refusalOf } from '@/lib/graphql/refusal';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

const MonitorTargetCollectionWatchDocument = graphql(`
  subscription MonitorTargetCollectionWatch {
    monitorTargetCollectionWatch {
      type
      target {
        id
        integration
        label
        clusterID
        currentUID
        enabled
        readState
        unknownSignals {
          name
          reason
        }
      }
    }
  }
`);

const IncidentCollectionWatchDocument = graphql(`
  subscription IncidentCollectionWatch {
    incidentCollectionWatch {
      type
      incident {
        id
        targetID
        targetUID
        subjectLabel
        subjectAttributes {
          label
          value
        }
        signal
        signalLabel
        severity
        firstSeenAt
        open
        occurrenceCount
        lastStartedAt
        lastSeenAt
        lastEndReason
        baseline
      }
    }
  }
`);

const MonitorSignalListDocument = graphql(`
  query MonitorSignalList {
    monitorSignalList {
      name
      integration
      label
      severity
    }
  }
`);

const IncidentOccurrenceEventFeedDocument = graphql(`
  subscription IncidentOccurrenceEventFeed($incidentID: IncidentID!) {
    incidentOccurrenceEventFeed(incidentID: $incidentID) {
      type
      incidentID
      occurrence {
        id
        incidentID
        number
        startedAt
        endedAt
        endReason
        detail {
          label
          value
        }
      }
    }
  }
`);

// Answered through `monitorTargetCollectionWatch`, which is what a switch draws.
const MonitorTargetEnabledSetDocument = graphql(`
  mutation MonitorTargetEnabledSet($id: MonitorTargetID!, $enabled: Boolean!) {
    monitorTargetEnabledSet(id: $id, enabled: $enabled) {
      id
      enabled
    }
  }
`);

// Each item as its watch delivers it. NonNullable: the item is null only on a Bookmark
// or a change whose record errored, which the reducers drop.
export type MonitorTarget = NonNullable<
  MonitorTargetCollectionWatchSubscription['monitorTargetCollectionWatch']['target']
>;
export type MonitorUnknownSignal = MonitorTarget['unknownSignals'][number];
export type Incident = NonNullable<IncidentCollectionWatchSubscription['incidentCollectionWatch']['incident']>;
export type MonitorSignal = MonitorSignalListQuery['monitorSignalList'][number];
type OccurrenceFrame = IncidentOccurrenceEventFeedSubscription['incidentOccurrenceEventFeed'];
export type IncidentOccurrence = NonNullable<OccurrenceFrame['occurrence']>;

export const INTEGRATION_LABEL: Record<MonitorIntegration, string> = { Kubernetes: 'Kubernetes' };

// Why a signal could not be judged, as a line above a target's incidents reads it.
export const READ_ERROR_TEXT: Record<MonitorReadError, string> = {
  NotReady: 'still loading',
  Disabled: 'switched off',
  Unavailable: 'unreachable',
  PermissionDenied: 'access denied',
  Unauthenticated: 'signed out',
  RateLimited: 'rate limited',
  Unsupported: 'not supported',
  Truncated: 'answer cut short',
  Timeout: 'timed out',
  Invalid: 'answer out of bounds',
};

export const END_REASON_TEXT: Record<IncidentOccurrenceEndReason, string> = {
  Resolved: 'Resolved',
  Stopped: 'Stopped',
  Stale: 'Not seen for 24h',
};

// The incident's feed keeps what the sidecar retains.
export const MAX_OCCURRENCES = 100;

// How loud a severity's dot is, in the cluster health's tones.
export const SEVERITY_TONE: Record<MonitorSeverity, Tone> = { Critical: 'error', Warning: 'attention', Info: 'muted' };

const SEVERITY_RANK: Record<MonitorSeverity, number> = { Critical: 0, Warning: 1, Info: 2 };

// A missing time sorts last.
const timeOf = (at: string | null) => (at === null ? 0 : Date.parse(at));
const newestFirst = (a: string | null, b: string | null) => timeOf(b) - timeOf(a);

// Open first, the loudest then the newest start; then ended, the newest last sighting.
function compareInListOrder(a: Incident, b: Incident): number {
  if (a.open !== b.open) return a.open ? -1 : 1;
  if (!a.open) return newestFirst(a.lastSeenAt, b.lastSeenAt);
  return SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] || newestFirst(a.lastStartedAt, b.lastStartedAt);
}

const reduceTargets = collectionWatchReducer(
  ({ monitorTargetCollectionWatch: { type, target } }: MonitorTargetCollectionWatchSubscription) => ({
    type,
    item: target,
  }),
);

const reduceIncidents = collectionWatchReducer(
  ({ incidentCollectionWatch: { type, incident } }: IncidentCollectionWatchSubscription) => ({
    type,
    item: incident,
  }),
);

type IncidentsContextValue = {
  // Both null until both watches' Bookmarks and the clusters list have landed.
  targets: MonitorTarget[] | null;
  // The incidents of `targets`, in list order.
  incidents: Incident[] | null;
  signals: ReadonlyMap<string, MonitorSignal>;
};

const IncidentsContext = createContext<IncidentsContextValue | null>(null);
// Apart from the reads, so a press redraws only the switches.
const EnabledSetContext = createContext<MonitorTargetEnabledSet | null>(null);

// The event that tells this window a notification's press waits for it. It carries
// nothing: the press is taken with `notification_press_take`.
const PRESS_WAITING_EVENT = 'notification-press-waiting';

// A notification's press, as the host holds it for this window: the incident to open
// the Incidents dialog on, or none for a summary.
type Press = { incidentID: string | null };

// Opens the Incidents dialog on each press the host holds for this window: one made
// before this page could listen, taken as it mounts, and each it is told of after.
function useNotificationPresses() {
  const openDialog = useDialogHost()?.openDialog;
  useEffect(() => {
    if (!openDialog) return undefined;
    let active = true;
    let unlisten: (() => void) | undefined;
    const take = () =>
      invoke<Press | null>('notification_press_take')
        .then((press) => {
          if (active && press) openDialog('incidents', press.incidentID ?? undefined);
        })
        .catch(reportHostError);
    // The first take waits for the listener, so a press made between the two is
    // told. Async, so a webview with no host rejects rather than throws.
    (async () => getCurrentWebviewWindow().listen(PRESS_WAITING_EVENT, () => void take()))()
      .then((fn) => {
        if (!active) return fn();
        unlisten = fn;
        return take();
      })
      .catch(reportHostError);
    return () => {
      active = false;
      unlisten?.();
    };
  }, [openDialog]);
}

export function IncidentsProvider({ children }: { children: React.ReactNode }) {
  useNotificationPresses();
  const { data: targetWatch } = useFoldedSubscription({ query: MonitorTargetCollectionWatchDocument }, reduceTargets);
  const { data: incidentWatch } = useFoldedSubscription({ query: IncidentCollectionWatchDocument }, reduceIncidents);
  // Fixed for the life of the sidecar, so it is asked once.
  const [{ data: signalData }] = useQuery({ query: MonitorSignalListDocument });
  const { clusters } = useClusters();

  const targetMap = targetWatch?.snapshotComplete ? targetWatch.items : undefined;
  const incidentMap = incidentWatch?.snapshotComplete ? incidentWatch.items : undefined;

  // A cluster's target can outlive its cluster by one reconcile; the clusters list
  // already drops a cluster awaiting deletion, so its target goes with it. Targets
  // are sorted only when they or the clusters change, not on each incident batch.
  const targets = useMemo(() => {
    if (!targetMap || !clusters) return null;
    const clusterIDs = new Set(clusters.map((c) => c.id));
    return [...targetMap.values()]
      .filter((t) => t.clusterID === null || clusterIDs.has(t.clusterID))
      .toSorted((a, b) => a.label.localeCompare(b.label));
  }, [targetMap, clusters]);

  const incidents = useMemo(() => {
    if (!targets || !incidentMap) return null;
    const targetIDs = new Set(targets.map((t) => t.id));
    return [...incidentMap.values()].filter((i) => targetIDs.has(i.targetID)).toSorted(compareInListOrder);
  }, [targets, incidentMap]);

  const signalList = signalData?.monitorSignalList;
  const signals = useMemo(() => new Map((signalList ?? []).map((s) => [s.name, s])), [signalList]);

  // The targets wait for the incidents: a target drawn before them would read as having none.
  const value = useMemo(
    () => ({ targets: incidents ? targets : null, incidents, signals }),
    [targets, incidents, signals],
  );
  // Held here, not in Settings' Monitoring tab, so a switch in flight and its refusal
  // outlive the tab.
  const enabledSet = useEnabledSetState();
  return (
    <IncidentsContext.Provider value={value}>
      <EnabledSetContext.Provider value={enabledSet}>{children}</EnabledSetContext.Provider>
    </IncidentsContext.Provider>
  );
}

function useIncidentsContext(): IncidentsContextValue {
  const ctx = useContext(IncidentsContext);
  if (!ctx) throw new Error('monitoring hooks must be used inside <IncidentsProvider>');
  return ctx;
}

/** Every target whose cluster, if it has one, is listed, by label; null until all three snapshots land. */
export function useMonitorTargets(): MonitorTarget[] | null {
  return useIncidentsContext().targets;
}

/** The incidents of `useMonitorTargets()`'s targets, or of one of them, in list order. */
export function useIncidents(targetID?: string): Incident[] | null {
  const { incidents } = useIncidentsContext();
  return useMemo(
    () => (targetID === undefined ? incidents : (incidents?.filter((i) => i.targetID === targetID) ?? null)),
    [incidents, targetID],
  );
}

/** Every signal this build can find, by name; empty until the list answers. */
export function useMonitorSignals(): ReadonlyMap<string, MonitorSignal> {
  return useIncidentsContext().signals;
}

// The history, tagged with the incident it folded for.
type OccurrenceHistory = EventFeedHistory<IncidentOccurrence> & { incidentID: string | undefined };

const reduceOccurrences = eventFeedReducer(
  (frame: OccurrenceFrame) => ({ type: frame.type, event: frame.occurrence }),
  {
    compare: (a, b) => b.number - a.number,
    cap: MAX_OCCURRENCES,
  },
);

const NO_OCCURRENCES: IncidentOccurrence[] = [];

/**
 * One incident's retained occurrences, highest `number` first, unshared so a detail
 * opened late still gets the snapshot. A frame for another incident, a late
 * `Bookmark` included, is dropped, so a switch stays loading until its own arrives.
 */
export function useIncidentOccurrenceEventFeed(incidentID: string | undefined): {
  occurrences: IncidentOccurrence[];
  snapshotComplete: boolean;
} {
  const { data } = useFoldedSubscription(
    {
      query: IncidentOccurrenceEventFeedDocument,
      variables: { incidentID: incidentID ?? '' },
      pause: incidentID === undefined,
    },
    (prev: OccurrenceHistory | undefined, frames: IncidentOccurrenceEventFeedSubscription[]) => {
      const ownHistory = prev?.incidentID === incidentID ? prev : undefined;
      const ownFrames = frames
        .map(({ incidentOccurrenceEventFeed }) => incidentOccurrenceEventFeed)
        .filter((frame) => frame.incidentID === incidentID);
      return { ...reduceOccurrences(ownHistory, ownFrames), incidentID };
    },
    { unshared: true },
  );
  // The accumulator still holds the last incident's history for the render the id flips in.
  const history = data?.incidentID === incidentID ? data : undefined;
  return { occurrences: history?.events ?? NO_OCCURRENCES, snapshotComplete: history?.snapshotComplete ?? false };
}

export type MonitorTargetEnabledSet = {
  /** The targets whose switch is in flight, by id. */
  changing: ReadonlySet<string>;
  /** Each target's last refused switch, by id, until that target's next press. */
  errors: ReadonlyMap<string, string>;
  /** Sends nothing for a target already in flight. */
  set: (id: string, enabled: boolean) => Promise<void>;
};

/**
 * Turns a target's monitoring on or off. The switch draws the watch's `enabled`, which
 * the target's `Modified` moves, so this holds only what is in flight and what failed.
 */
export function useMonitorTargetEnabledSet(): MonitorTargetEnabledSet {
  const ctx = useContext(EnabledSetContext);
  if (!ctx) throw new Error('monitoring hooks must be used inside <IncidentsProvider>');
  return ctx;
}

function useEnabledSetState(): MonitorTargetEnabledSet {
  const [, send] = useMutation(MonitorTargetEnabledSetDocument);
  const [changing, setChanging] = useState<ReadonlySet<string>>(new Set());
  const [errors, setErrors] = useState<ReadonlyMap<string, string>>(new Map());
  // Read synchronously: two presses inside one render both see the same `changing`.
  const inFlight = useRef(new Set<string>());

  const set = useCallback(
    async (id: string, enabled: boolean) => {
      if (inFlight.current.has(id)) return;
      inFlight.current.add(id);
      setChanging(new Set(inFlight.current));
      setErrors((prev) => {
        if (!prev.has(id)) return prev;
        const next = new Map(prev);
        next.delete(id);
        return next;
      });
      const { error } = await send({ id, enabled });
      inFlight.current.delete(id);
      setChanging(new Set(inFlight.current));
      const refusal = refusalOf(error);
      if (refusal) setErrors((prev) => new Map(prev).set(id, refusal));
    },
    [send],
  );

  return useMemo(() => ({ changing, errors, set }), [changing, errors, set]);
}

/** Whether the OS shows Kubetail's notifications, as the host last read it. */
export type NotificationPermission = 'Granted' | 'Denied' | 'NotDetermined';

/** The host's answer, kept current; undefined until its first frame. */
export function useNotificationPermission(): NotificationPermission | undefined {
  const [permission, setPermission] = useState<NotificationPermission>();
  useEffect(() => {
    let active = true;
    const channel = new Channel<NotificationPermission>((value) => {
      if (active) setPermission(value);
    });
    invoke('notification_permission_state_watch', { channel }).catch(reportHostError);
    return () => {
      active = false;
    };
  }, []);
  return permission;
}

// What a target's line above its incidents says, the first that holds.
export type TargetStatus =
  | { kind: 'off' }
  | { kind: 'failed'; reasons: MonitorReadError[] }
  | { kind: 'never-read' }
  | { kind: 'partial'; unknownSignals: MonitorUnknownSignal[] };

/** A target's line; null for a target that is on and judged everything. */
export function targetStatus(target: MonitorTarget): TargetStatus | null {
  if (!target.enabled) return { kind: 'off' };
  switch (target.readState) {
    case 'Failed':
      return { kind: 'failed', reasons: [...new Set(target.unknownSignals.map((s) => s.reason))] };
    case 'NeverRead':
      return { kind: 'never-read' };
    case 'Partial':
      return { kind: 'partial', unknownSignals: target.unknownSignals };
    case 'Ok':
      return null;
  }
}

/**
 * An incident's state and the time it is read from. *Resolved* only when a reading saw
 * it recover: nobody saw a `Stopped` or `Stale` incident do so.
 */
export function incidentState(incident: Incident): { label: string; at: string } {
  if (incident.open || !incident.lastSeenAt) return { label: 'Seen since', at: incident.lastStartedAt };
  return {
    label: incident.lastEndReason === 'Resolved' ? 'Resolved, last seen' : 'Last seen',
    at: incident.lastSeenAt,
  };
}

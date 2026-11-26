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

// The one place the app makes a log `Client`: `logsFetch` and `logsTail` over the app's urql
// client, for one view of one cluster. Cursors pass through as the sidecar wrote them. Operation
// errors reach the error bus through `errorReportExchange`; a page's also rejects its fetch, and
// a tail's calls `onFailed`, for the window to see.

import type { Client as UrqlClient } from 'urql';

import { graphql } from '@/gql';
import type {
  LogsFetchInput,
  LogsFetchQuery as LogsFetchResult,
  LogsTailSubscription as LogsTailResult,
  LogsScopeInput,
} from '@/gql/graphql';
import { refusalOf } from '@/lib/graphql/refusal';
import { containerKey, previousNanosecond } from '@/lib/log-viewer';
import type { Client, Cursor, LogLine, Page } from '@/lib/log-viewer';
import type { LogsViewAction } from '@/lib/logs-view';

// The viewer keeps its own request, so a page's echo of it is not selected.
const LogsFetchQuery = graphql(`
  query LogsFetch($id: ClusterID!, $request: LogsFetchInput!) {
    logsFetch(id: $id, request: $request) {
      records {
        timestamp
        message
        truncatedBytes
        cursor
        stream {
          namespace
          pod
          container
          previous
        }
        place {
          node
          region
          zone
          os
          arch
        }
        gap {
          after
          before
        }
      }
      page {
        prev
        next
        follow
      }
      notices {
        code
        message
      }
    }
  }
`);

const LogsTailDocument = graphql(`
  subscription LogsTail($id: ClusterID!, $request: LogsTailInput!) {
    logsTail(id: $id, request: $request) {
      type
      record {
        timestamp
        message
        truncatedBytes
        cursor
        stream {
          namespace
          pod
          container
          previous
        }
        place {
          node
          region
          zone
          os
          arch
        }
        gap {
          after
          before
        }
      }
      notice {
        code
        message
      }
      ended {
        code
        message
      }
    }
  }
`);

// Field by field, since the action came off a query and carries `__typename`s an input refuses.
function scopeInput({ sources, filters, grep }: LogsViewAction): LogsScopeInput {
  return {
    sources: sources.map(({ namespace, kind, name, containers, allContainers, previous }) => ({
      namespace,
      kind,
      name,
      containers,
      allContainers,
      previous,
    })),
    filters: filters.map(({ field, values }) => ({ field, values })),
    grep,
  };
}

// Field by field for the same reason: the library's types are the wire's without `__typename`.
type LogRecord = NonNullable<LogsTailResult['logsTail']['record']>;

function lineOf({ timestamp, message, truncatedBytes, cursor, stream, place, gap }: LogRecord): LogLine {
  return {
    timestamp,
    message,
    truncatedBytes,
    cursor,
    stream: { namespace: stream.namespace, pod: stream.pod, container: stream.container, previous: stream.previous },
    place: { node: place.node, region: place.region, zone: place.zone, os: place.os, arch: place.arch },
    gap: gap ? { after: gap.after, before: gap.before } : null,
  };
}

type LogsPage = LogsFetchResult['logsFetch'];

function pageOf({ records, page, notices }: LogsPage): Page {
  return {
    lines: records.map(lineOf),
    edges: { prev: page.prev, next: page.next, follow: page.follow },
    anchor: null,
    notices: notices.map(({ code, message }) => ({ code, message })),
  };
}

export function createLogsClient(urql: UrqlClient, clusterId: string, action: LogsViewAction): Client {
  const scope = scopeInput(action);

  // A page's variables repeat — a landing read again, a `follow` polled again — and each must
  // reach the sidecar, never the cache's last answer.
  const read = async (request: LogsFetchInput): Promise<Page> => {
    const { data, error } = await urql
      .query(LogsFetchQuery, { id: clusterId, request }, { requestPolicy: 'network-only' })
      .toPromise();
    if (!data) throw new Error(refusalOf(error) ?? 'the sidecar answered no page');
    return pageOf(data.logsFetch);
  };

  return {
    fetch: async (query) => {
      const { limit: pageSize } = query;
      if (query.kind === 'Continue') return read({ cursor: query.cursor, seen: query.seen, pageSize });
      if (query.kind === 'Until') return read({ scope, until: query.at, seen: query.seen, pageFrom: 'Last', pageSize });
      if (query.kind === 'Since')
        return read({ scope, since: query.at, seen: query.seen, pageFrom: 'First', pageSize });
      if (query.kind !== 'At') return read({ scope, pageFrom: query.kind, pageSize });
      // Two disjoint halves: the older ends the nanosecond before the moment, so every line at
      // it is the newer's, and a limit cutting each at its own end leaves no hole between.
      const [older, newer] = await Promise.all([
        read({ scope, until: previousNanosecond(query.at), pageFrom: 'Last', pageSize }),
        read({ scope, since: query.at, pageFrom: 'First', pageSize }),
      ]);
      return {
        lines: [...older.lines, ...newer.lines],
        edges: { prev: older.edges.prev, next: newer.edges.next, follow: newer.edges.follow },
        anchor: older.lines.length,
        notices: [...older.notices, ...newer.notices],
      };
    },

    tail: (cursor, seen, onFrame, onFailed) => {
      // A reconnect goes on from the same cursor, past the newest line it handed on of each
      // container. The given `seen` is opaque, so it goes too: the sidecar keeps the newer of
      // two lines of one container.
      const newest = new Map<string, Cursor>();
      const request = () => ({ cursor, seen: [...seen, ...newest.values()] });
      let ended = false;
      const subscription = urql
        .subscription(
          LogsTailDocument,
          { id: clusterId, request: request() },
          { reconnectVariables: () => ({ id: clusterId, request: request() }) },
        )
        .subscribe(({ data, error }) => {
          if (ended) return;
          const frame = data?.logsTail;
          if (!frame) {
            // A tail the sidecar could not open answers errors and no frame, then completes.
            // Stop here, before the reconnect the `complete` brings: the window reopens it
            // from a page. A tail that dies once open is the exchange's to reconnect.
            if (error) {
              ended = true;
              subscription.unsubscribe();
              onFailed(new Error(refusalOf(error) ?? error.message));
            }
            return;
          }
          if (frame.type === 'Line') {
            if (frame.record) {
              const line = lineOf(frame.record);
              newest.set(containerKey(line.stream), line.cursor);
              onFrame({ kind: 'Line', line });
            }
          } else if (frame.type === 'Notice') {
            if (frame.notice)
              onFrame({ kind: 'Notice', notice: { code: frame.notice.code, message: frame.notice.message } });
          } else {
            // Tailing again would end the same way, so the reconnect the server's
            // `complete` brings must not happen.
            ended = true;
            onFrame({ kind: 'Ended', reason: frame.ended?.message ?? '' });
            subscription.unsubscribe();
          }
        });
      return () => {
        ended = true;
        subscription.unsubscribe();
      };
    },
  };
}

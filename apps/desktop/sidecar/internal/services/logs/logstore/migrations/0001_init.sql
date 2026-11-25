-- Copyright 2024 The Kubetail Authors
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- One cluster cache's log lines. Every moment is nanoseconds since the epoch, and NULL
-- is not zero: an unknown time is NULL. A stream's and a chunk's id never come back
-- (AUTOINCREMENT): a tailer, a watermark and the janitor name a row by id across
-- transactions, and a reused one would hand them another stream's lines.

-- streams: one runtime instance of a container. A restart makes a new one, and the
-- old instance's lines stay under the old row. Whether it is the pod's previous instance
-- is read off the pod when a page is read, never stored. The place, the labels and the
-- times are a snapshot from the mirror taken while the pod is still there, since the
-- lines outlive it; they describe the stream and never name it.
--
-- pod_uid is the pod the instance ran in, '' when unknown, which tells a gone pod's
-- streams from a later pod of its name. is_default is 1 for the pod's default
-- container as kubectl picks it, which a view naming no container reads once the pod
-- is gone. container_id is the runtime instance, '' for a backend that knows only the
-- name. started_at is when the instance started, NULL when unknown; finished_at is when
-- it stopped, NULL while it runs. labels is the pod's labels as a JSON object, '{}'
-- when unknown.
CREATE TABLE streams (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  namespace    TEXT    NOT NULL,
  pod          TEXT    NOT NULL,
  pod_uid      TEXT    NOT NULL,
  container    TEXT    NOT NULL,
  is_default   INTEGER NOT NULL CHECK (is_default IN (0, 1)),
  container_id TEXT    NOT NULL,
  started_at   INTEGER,
  finished_at  INTEGER,
  node         TEXT    NOT NULL,
  region       TEXT    NOT NULL,
  zone         TEXT    NOT NULL,
  os           TEXT    NOT NULL,
  arch         TEXT    NOT NULL,
  labels       TEXT    NOT NULL,
  UNIQUE (namespace, pod, container, container_id)
) STRICT;

-- pod_owners: each controller above a pod, walked up its controller references in the
-- mirror, so a workload finds the streams of a pod it no longer runs. A pair is filed
-- with the pod's streams and never removed while the pod has one: the mirror can show
-- a pod before its ReplicaSet, and a later filing adds what that one could not.
CREATE TABLE pod_owners (
  pod_uid   TEXT NOT NULL,
  owner_uid TEXT NOT NULL,
  PRIMARY KEY (owner_uid, pod_uid)
) STRICT;

-- chunks: the unit of coverage and of eviction. A chunk states a fact: every line the
-- backend served for this stream between its two edges that matches pattern is in lines.
-- Its edges are positions, (from_ns, from_ord) and (to_ns, to_ord), since a chunk closes
-- at a byte budget wherever that falls, inside a moment included.
--
-- prev_ns and prev_ord are the position the chunk continues from: the last line of the
-- chunk before it, with no line between that the chunk's backend holds. NULL when the
-- chunk opened on a gap. Two chunks so linked are one span of coverage.
--
-- pattern is the grep the fill ran under, '' for every line. backend is the backend that
-- filled it, by name. at_beginning is 1 when that backend holds nothing older than from_ns;
-- at_end is 1 when to_ns is the last line of a finished instance's log; open is 1 while
-- a tailer is still writing into it. read_at_ms is when a page last read from it or a
-- fill wrote it, in unix milliseconds, NULL when neither has: a clock, not a line's
-- moment. A tailer's write leaves it alone, so a tailed stream nobody pages is
-- evictable from the moment it is written.
CREATE TABLE chunks (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  stream_id    INTEGER NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  pattern      TEXT    NOT NULL,
  from_ns      INTEGER NOT NULL,
  from_ord     INTEGER NOT NULL,
  to_ns        INTEGER NOT NULL,
  to_ord       INTEGER NOT NULL,
  prev_ns      INTEGER,
  prev_ord     INTEGER,
  backend      TEXT    NOT NULL,
  at_beginning INTEGER NOT NULL CHECK (at_beginning IN (0, 1)),
  at_end       INTEGER NOT NULL CHECK (at_end IN (0, 1)),
  open         INTEGER NOT NULL CHECK (open IN (0, 1)),
  bytes        INTEGER NOT NULL,
  read_at_ms   INTEGER,
  UNIQUE (stream_id, pattern, from_ns, from_ord)
) STRICT;

-- lines: one log line, keyed by its position, the three fields a cursor holds; ord is
-- the backend's own number for the line within its moment, so a line gets one key
-- whichever fill found it. message is after stripControl, so at most 16 KiB.
--
-- A rowid table rather than WITHOUT ROWID, since a message can run to 16 KiB. Nothing
-- reads a line by its id: every read walks lines_position.
CREATE TABLE lines (
  id        INTEGER PRIMARY KEY,
  stream_id INTEGER NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  ts_ns     INTEGER NOT NULL,
  ord       INTEGER NOT NULL,
  message   TEXT    NOT NULL
) STRICT;

CREATE UNIQUE INDEX lines_position ON lines (stream_id, ts_ns, ord);

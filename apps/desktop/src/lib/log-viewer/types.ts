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

// The contract between the window and whatever feeds it. Each type mirrors the wire's
// (`LogRecord`, `LogsFetchResult`, `LogNotice`, `LogsTailFrame`) field for field.

/** Opaque. The library hands a cursor back to the client that issued it and reads nothing from it. */
export type Cursor = string;

export type LogLine = {
  /** When the line was written: RFC 3339 in UTC (`Z`), with up to nine fraction digits. */
  timestamp: string;
  /** Names the line. A repeated line is dropped by it. */
  cursor: Cursor;
  /** The text. The library draws nothing and strips nothing: the client hands it text fit to show. */
  message: string;
  /** The bytes the server cut off `message` at the request's cap, 0 when whole. */
  truncatedBytes: number;
  /** The log the line came from. Its four fields together name it. */
  stream: { namespace: string; pod: string; container: string; previous: boolean };
  /** Where the stream's pod runs. Empty where unknown, and no part of the stream's name. */
  place: { node: string; region: string; zone: string; os: string; arch: string };
  /**
   * Lines of the stream that may be missing just before this one, null when none are: the
   * stamps, RFC 3339, of the last known line before the hole and the first known line past it.
   */
  gap: { after: string; before: string } | null;
};

/** Where the window is told to be. `at` is the moment, RFC 3339, set for `At` alone. */
export type Anchor = { kind: 'Head' | 'Tail' | 'At'; at: string | null };

/**
 * One read. `First` and `Last` read from an end, `At` reads `limit` lines either side of a
 * moment, and `Continue` reads on from a page's edge, past each line in `seen` for its container.
 * `Until` reads the `limit` newest lines at or before a moment, each container in `seen` strictly
 * before its line, and `Since` the `limit` oldest at or after it, each strictly past its line.
 */
export type FetchQuery =
  | { kind: 'First'; limit: number }
  | { kind: 'Last'; limit: number }
  | { kind: 'At'; at: string; limit: number }
  | { kind: 'Until'; at: string; seen: Cursor[]; limit: number }
  | { kind: 'Since'; at: string; seen: Cursor[]; limit: number }
  | { kind: 'Continue'; cursor: Cursor; seen: Cursor[] | null; limit: number };

export type Notice = { code: string; message: string };

export type Page = {
  /** Oldest first, as the client sorted them, whatever the query. */
  lines: LogLine[];
  /**
   * Where a page that way goes on, null with nothing that way. `follow` is the live end: set on a
   * page that read every stream to its end. An `Until`'s `prev` reads each container in its
   * `seen` before its line on every page back.
   */
  edges: { prev: Cursor | null; next: Cursor | null; follow: Cursor | null };
  /** For `At`: the index of the first line at or after the moment, `lines.length` with none. Null otherwise. */
  anchor: number | null;
  /** What the page could not do, one per notice, for the viewer to show. */
  notices: Notice[];
};

/** `Ended` is the last frame: the tail is over, and tailing again would end the same way. */
export type TailFrame =
  | { kind: 'Line'; line: LogLine }
  | { kind: 'Notice'; notice: Notice }
  | { kind: 'Ended'; reason: string };

/** Where lines come from. Anything that implements it can feed the window. */
export type Client = {
  fetch: (query: FetchQuery) => Promise<Page>;
  /**
   * Calls back per frame from a page's `next` or `follow`, past each line in `seen` for its
   * container, until the function it answers is called. A tail that cannot open throws, or,
   * when it learns so later, calls `onFailed` once and sends nothing after.
   */
  tail: (
    cursor: Cursor,
    seen: Cursor[],
    onFrame: (frame: TailFrame) => void,
    onFailed: (error: unknown) => void,
  ) => () => void;
};

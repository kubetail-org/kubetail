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

// The time since a moment, as kubectl prints an age: the largest unit alone (45s, 22m, 3d),
// ticking.
import ReactTimeAgo from 'react-timeago';
import type { Formatter } from 'react-timeago';

const UNIT: Record<string, string> = {
  second: 's',
  minute: 'm',
  hour: 'h',
  day: 'd',
  week: 'w',
  month: 'mo',
  year: 'y',
};
const shortAge: Formatter = (value, unit) => `${value}${UNIT[unit]}`;

// The time since `ms`, or a dash for a time that did not parse. Muted unless the caller
// styles it.
export function Age({ ms, className = 'text-xs text-muted-foreground' }: { ms: number; className?: string }) {
  return (
    <span className={className}>
      {Number.isNaN(ms) ? '—' : <ReactTimeAgo date={ms} component="span" formatter={shortAge} maxPeriod={60} />}
    </span>
  );
}

// A timestamp off the wire in milliseconds: NaN when absent or unparsable, as `Age` expects.
export function timestampMs(timestamp: string | null | undefined): number {
  return timestamp ? Date.parse(timestamp) : Number.NaN;
}

// Youngest first.
export function ageSortValue(ms: number): number | undefined {
  return Number.isNaN(ms) ? undefined : -ms;
}

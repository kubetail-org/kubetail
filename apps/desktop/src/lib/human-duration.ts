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

// kubectl's HumanDuration: finer units while they still say something.
export function humanDuration(ms: number): string {
  const s = Math.floor(Math.max(0, ms) / 1000);
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  if (s < 120) return `${s}s`;
  if (m < 10) return `${m}m${s % 60 ? `${s % 60}s` : ''}`;
  if (m < 180) return `${m}m`;
  if (h < 8) return `${h}h${m % 60 ? `${m % 60}m` : ''}`;
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

// humanDuration by the whole minute ("<1m", "5m", "3h20m"), for a span drawn
// beside one that ticks each second.
export function humanMinutes(ms: number): string {
  const m = Math.floor(ms / 60_000);
  if (m < 1) return '<1m';
  // humanDuration counts the second minute in seconds.
  if (m < 2) return '1m';
  return humanDuration(m * 60_000);
}

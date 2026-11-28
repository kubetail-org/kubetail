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

// A detail row holding a moment and how long ago it was, ticking: 2026-07-20 10:04 · 2h ago.
import { useState } from 'react';

import { Age } from '@/components/dashboard/age';
import { DetailRow } from '@/components/dashboard/detail-row';
import { humanDuration } from '@/lib/human-duration';

const pad = (n: number) => String(n).padStart(2, '0');

// A time as the user's clock reads it, to the minute: 2026-07-20 10:04.
function localMinute(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// A dash for a time that did not parse. A title cannot tick, so it reads the clock as the
// pointer arrives.
export function SeenRow({ label, ms }: { label: string; ms: number }) {
  const [hoveredAt, setHoveredAt] = useState<number>();
  if (Number.isNaN(ms)) return <DetailRow label={label}>—</DetailRow>;
  const at = localMinute(ms);
  const title = hoveredAt === undefined ? at : `${at} · ${humanDuration(hoveredAt - ms)} ago`;
  return (
    <DetailRow label={label} title={title} onPointerEnter={() => setHoveredAt(Date.now())}>
      {at} · <Age ms={ms} className="" /> ago
    </DetailRow>
  );
}

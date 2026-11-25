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

import { useState } from 'react';

// A value derived from watches, as it stood when they last held a whole snapshot. A
// reconnect masks a watch's data until its new snapshot lands, so a view drawn from it
// would empty and refill, unmounting whatever the user was in the middle of (an edit, a
// confirm). Before the first snapshot there is nothing to hold, and the value passes through.
export function useLastSettled<T>(value: T, settled: boolean): T {
  // Boxed, so a held `undefined` differs from nothing held.
  const [held, setHeld] = useState<{ value: T } | null>(null);
  if (settled && (!held || held.value !== value)) setHeld({ value });
  if (settled || !held) return value;
  return held.value;
}

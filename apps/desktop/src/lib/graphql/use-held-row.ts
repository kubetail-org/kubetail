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

// A row a view has open, as it reads it by id off a watch's rows: absent once the row is
// gone, or while a reconnect masks the rows. `shown` is the last body seen, held through
// both. `gone` holds from the first settled rows that lack the row (the cache's whole
// present, after its snapshot's Bookmark), through a reconnect, until the row comes back.
// The view is keyed on the row's id, so what it holds is that row's.
export function useHeldRow<T extends object>(
  row: T | undefined,
  settled: boolean,
): { shown: T | undefined; gone: boolean } {
  const [held, setHeld] = useState(row);
  if (row && row !== held) setHeld(row);
  const [gone, setGone] = useState(false);
  if (row && gone) setGone(false);
  if (!row && settled && !gone) setGone(true);
  return { shown: row ?? held, gone };
}

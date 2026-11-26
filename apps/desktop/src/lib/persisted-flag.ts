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

// A pane's open state, remembered. Windows share one localStorage but never sync
// live — toggling in one leaves the others untouched; a new window opens the way
// the last one was left, the same bargain `usePersistedWidth` makes for widths.
import { useCallback, useState } from 'react';

import { storageKey } from '@/lib/storage-key';

export function usePersistedFlag(name: string, initial: boolean): [boolean, (value: boolean) => void] {
  const key = storageKey(name);

  const read = useCallback(() => {
    const stored = localStorage.getItem(key);
    return stored === null ? initial : stored === 'true';
  }, [initial, key]);

  // The key travels with the state, so a caller that switches names (a pane whose
  // state is kept per mode) reads the new one's state instead of carrying the old
  // one's across.
  const [state, setState] = useState(() => ({ key, value: read() }));
  if (state.key !== key) setState({ key, value: read() });

  const set = useCallback(
    (value: boolean) => {
      setState({ key, value });
      localStorage.setItem(key, String(value));
    },
    [key],
  );

  return [state.value, set];
}

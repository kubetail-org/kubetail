// Copyright 2024-2026 The Kubetail Authors
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

import { useCallback, useLayoutEffect, useRef } from 'react';

export type BeforePaintCallback = () => void | Promise<void>;

export type BeforePaintSubscribe = (callback: BeforePaintCallback) => Promise<void>;

type BeforePaintQueueItem = {
  callback: BeforePaintCallback;
  resolve: () => void;
  reject: (error: unknown) => void;
};

export function useBeforePaint(trigger: any) {
  const beforePaintQueueRef = useRef<BeforePaintQueueItem[]>([]);

  useLayoutEffect(() => {
    if (!beforePaintQueueRef.current.length) return;
    const queue = beforePaintQueueRef.current;
    beforePaintQueueRef.current = [];
    for (let i = 0; i < queue.length; i += 1) {
      const { callback, resolve, reject } = queue[i];
      try {
        const result = callback();
        Promise.resolve(result).then(resolve).catch(reject);
      } catch (error) {
        reject(error);
      }
    }
  }, [trigger]);

  return useCallback(
    (cb: BeforePaintCallback) =>
      new Promise<void>((resolve, reject) => {
        beforePaintQueueRef.current.push({ callback: cb, resolve, reject });
      }),
    [],
  );
}

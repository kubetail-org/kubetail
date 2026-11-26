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

// The fold every keyed state watch shares. See docs/read-contracts/README.md, *Keyed state watch*.
import type { Keyed } from './collection-watch';
import type { SubscriptionReducer } from './use-folded-subscription';

// Each frame replaces its key's value. There is no removal frame, so the subscriber takes
// membership from the collection watch that owns the keys.
export function keyedStateWatchReducer<Data, Value>(
  select: (data: Data) => Value,
  keyOf: (value: Value) => string,
): SubscriptionReducer<Data, Keyed<Value>> {
  return (prev, frames) => {
    const values = new Map(prev);
    frames.forEach((data) => {
      const value = select(data);
      values.set(keyOf(value), value);
    });
    return values;
  };
}

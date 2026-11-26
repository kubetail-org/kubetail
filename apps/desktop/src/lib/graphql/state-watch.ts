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

// The fold every state watch shares. See docs/read-contracts/README.md, *State watch*.
import { perFrame } from './use-folded-subscription';
import type { SubscriptionReducer } from './use-folded-subscription';

// Every frame is the whole value, so each replaces what is held. Undefined until the first
// frame lands is what tells "not known yet" from an empty value.
export function stateWatchReducer<Data, Value>(select: (data: Data) => Value): SubscriptionReducer<Data, Value> {
  return perFrame((_prev, data) => select(data));
}

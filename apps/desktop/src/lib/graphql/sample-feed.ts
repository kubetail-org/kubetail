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

// The fold every sample feed shares. See docs/read-contracts/README.md, *Sample feed*.
import { perFrame } from './use-folded-subscription';
import type { SubscriptionReducer } from './use-folded-subscription';

// One frame of a sample feed: the whole retained window first, then the samples since.
export type SampleFeedFrame<Sample> = { intervalSeconds: number; windowSeconds: number; samples: Sample[] };

// How many samples a feed holds: its window at its interval, and never fewer than one, so a
// chart and the fold agree on the count.
export function sampleFeedSlots({ intervalSeconds, windowSeconds }: SampleFeedFrame<unknown>): number {
  return Math.max(1, Math.floor(windowSeconds / intervalSeconds));
}

// Appends each frame's samples and keeps the last sampleFeedSlots. It reads no
// timestamps, so a wall clock that steps cannot drop or reorder a sample. The first frame
// folds to a feed even when empty, which is how a reader tells "empty" from "not yet".
export function sampleFeedReducer<Data, Sample>(
  select: (data: Data) => SampleFeedFrame<Sample>,
): SubscriptionReducer<Data, SampleFeedFrame<Sample>> {
  return perFrame((prev, data) => {
    const frame = select(data);
    return {
      ...frame,
      samples: [...(prev?.samples ?? []), ...frame.samples].slice(-sampleFeedSlots(frame)),
    };
  });
}

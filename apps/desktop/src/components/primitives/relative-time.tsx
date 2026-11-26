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

import { Elapsed } from '@/components/primitives/elapsed';
import { humanDuration } from '@/lib/human-duration';

const ago = (ms: number) => `${humanDuration(ms)} ago`;

// The time since `ms` ("45s ago"), ticking each second; `format` words it otherwise.
export function RelativeTime({ ms, format = ago }: { ms: number; format?: (ms: number) => string }) {
  return (
    <span>
      <Elapsed since={ms} format={format} />
    </span>
  );
}

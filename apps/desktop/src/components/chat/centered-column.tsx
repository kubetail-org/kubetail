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

// Opt-in per page: a full-bleed page simply doesn't use it. Window-frame
// concerns (background, height, title-bar band) belong to
// `src/layouts/app-layout.tsx`, not here.
import type { ReactNode } from 'react';

// `min-h-0`: a plain flex item's minimum is its content, and the message list below
// must be able to shrink to the frame and scroll.
export function CenteredColumn({ children }: { children?: ReactNode }) {
  return <div className="mx-auto flex min-h-0 w-full max-w-166 flex-1 flex-col gap-3 px-2 pt-6 pb-2">{children}</div>;
}

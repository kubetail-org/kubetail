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

// Whether a keydown belongs to an IME: while it composes, Enter accepts a candidate and
// Escape drops it. WebKit sends the accepting Enter after `compositionend`, no longer
// composing but with keyCode 229.
export function isImeKey(event: KeyboardEvent): boolean {
  // oxlint-disable-next-line typescript/no-deprecated -- keyCode is the one mark WebKit's accepting Enter carries
  return event.isComposing || event.keyCode === 229;
}

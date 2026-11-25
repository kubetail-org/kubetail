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

// Every key the app puts in a browser store is formed here. The `kubetail:` prefix
// marks ours apart from what `@kubetail/ui` persists — it writes an unprefixed
// `sidebar_state` cookie — and one funnel means a call site cannot forget it.
// Callers pass the name alone, so a key seen in devtools still greps to source.
export function storageKey(name: string): string {
  return `kubetail:${name}`;
}

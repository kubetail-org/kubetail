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

// What an object panel tab says in place of its content: not synced, loading, nothing here.
import type { ReactNode } from 'react';

export function ObjectPanelNote({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">{children}</div>;
}

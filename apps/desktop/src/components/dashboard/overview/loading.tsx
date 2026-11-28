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

// What a section draws until its watches have their snapshots: never a placeholder number.
import { Spinner } from '@kubetail/ui/elements/spinner';

export function Loading() {
  return (
    <div className="flex items-center gap-2 text-xs/4.5 text-muted-foreground">
      <Spinner size="sm" />
      Loading…
    </div>
  );
}

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
// The model's description of a call, drawn one way wherever it sits: italic, the
// text in a truncate span, and no title, since a native tooltip draws it raw. The
// request quotes it, with the quotes outside the span so the closing one survives
// the ellipsis; a settled call's row leads with its kind and draws it bare.
import { VisibleText } from '@/components/primitives/visible-text';

export function ModelDescription({ line, quoted = true }: { line: string; quoted?: boolean }) {
  return (
    <span className="flex min-w-0 italic">
      {quoted && <span>“</span>}
      <span className="min-w-0 truncate">
        <VisibleText text={line} />
      </span>
      {quoted && <span>”</span>}
    </span>
  );
}

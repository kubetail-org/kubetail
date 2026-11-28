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

// An integration's own labels and values, drawn as key–value text: each is text a
// target returned, so both go through VisibleText.
import { VisibleText } from '@/components/primitives/visible-text';

export function Attributes({ attributes }: { attributes: { label: string; value: string }[] }) {
  if (attributes.length === 0) return null;
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
      {attributes.map(({ label, value }) => (
        <div key={label} className="contents">
          <dt className="text-muted-foreground">
            <VisibleText text={label} />
          </dt>
          <dd className="min-w-0 break-all">
            <VisibleText text={value} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

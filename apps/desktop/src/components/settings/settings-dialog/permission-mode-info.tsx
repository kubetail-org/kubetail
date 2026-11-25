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

// The Default mode heading's info button: what holds whatever mode and rules
// the user picks. The two lists are the sidecar's; the fixed limits describe
// the engine as it is.
import { InfoPopover } from './group';

const FIXED = [
  "A command running in the background can't change the cluster",
  'A chat outside the sandbox asks before every command',
  "A write Kubetail can't record doesn't run",
];

export function PermissionModeInfo({ alwaysAsks, accessControl }: { alwaysAsks: string[]; accessControl: string[] }) {
  return (
    <InfoPopover label="What modes and rules cannot change" className="gap-0 p-0 text-sm">
      <p className="px-4 py-3 text-muted-foreground">
        Your modes and rules decide most changes. These hold whatever you choose.
      </p>
      <Limits
        title="Always asks, in every mode"
        note="A rule can make these refuse, never run unasked. A read-only account refuses them."
        lines={alwaysAsks}
      />
      <Limits
        title="Asks unless a rule inside a namespace allows it"
        note="Across a whole cluster or a cloud account these always ask."
        lines={accessControl}
      />
      <Limits title="No mode or rule changes these" lines={FIXED} />
    </InfoPopover>
  );
}

function Limits({ title, note, lines }: { title: string; note?: string; lines: string[] }) {
  return (
    <section aria-label={title}>
      <h5 className="border-y bg-muted px-4 py-1.5 text-muted-foreground">{title}</h5>
      <div className="flex flex-col gap-1 px-4 py-3">
        {note && <p className="text-muted-foreground">{note}</p>}
        <ul>
          {lines.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      </div>
    </section>
  );
}

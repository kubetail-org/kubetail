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

// A titled card in a detail pane: a header with an optional note on its right, over a body.
import type { ReactNode } from 'react';

type DetailCardProps = {
  title: ReactNode;
  // Muted text on the header's right: a count, a class.
  note?: ReactNode;
  children: ReactNode;
};

export function DetailCard({ title, note, children }: DetailCardProps) {
  return (
    <section className="shrink-0 overflow-hidden rounded-lg border">
      <header className="flex h-10 items-center justify-between gap-2 border-b bg-card px-3 py-2.5">
        <h3 className="truncate text-[13px]/4.75 font-semibold">{title}</h3>
        {note !== undefined && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{note}</span>}
      </header>
      {children}
    </section>
  );
}

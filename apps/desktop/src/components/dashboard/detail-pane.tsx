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
import { Button } from '@kubetail/ui/elements/button';
import { cn } from '@kubetail/ui/lib/utils';
import { X } from 'lucide-react';
import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';

type DetailPaneProps = {
  // Names the pane: the kind of thing open in it.
  label: string;
  // The header's left, which stays put over the body.
  title?: ReactNode;
  // The header's controls before the close button.
  actions?: ReactNode;
  closeLabel: string;
  // A muted line under the header, such as that the open row is gone.
  note?: string;
  onClose: () => void;
  className?: string;
  // The body, which scrolls.
  children?: ReactNode;
};

// An open row in full, in the column beside the list. The caller keys it on the row's id,
// so it mounts on each row opened.
export function DetailPane({ label, title, actions, closeLabel, note, onClose, className, children }: DetailPaneProps) {
  // The control that opened it is gone or elsewhere, so focus moves here, where Escape closes it.
  const ref = useRef<HTMLElement>(null);
  useEffect(() => ref.current!.focus(), []);

  return (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- it hears Escape from the controls inside it, which keep their own keys
    <section
      ref={ref}
      aria-label={label}
      tabIndex={-1}
      className={cn('flex min-h-0 flex-col gap-3 outline-none', className)}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      <header className="flex shrink-0 items-start gap-2 pr-2.5">
        <div className="min-w-0 flex-1">{title}</div>
        <div className="flex shrink-0 items-center gap-1">
          {actions}
          <Button type="button" variant="ghost" size="icon-sm" aria-label={closeLabel} onClick={onClose}>
            <X className="size-4" aria-hidden />
          </Button>
        </div>
      </header>
      {note && <p className="pr-2.5 text-xs text-muted-foreground">{note}</p>}
      {children && <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto pr-2.5">{children}</div>}
    </section>
  );
}

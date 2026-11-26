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

// The app's reusable centered dialogs over @kubetail/ui's base-ui `Dialog*`
// primitives. `Dialog` draws a standard header over a viewport-capped scrolling
// body; `PanelDialog` draws a bare frame whose children lay out the whole window
// and draw its `DialogTitle`. Both are fully controlled (no built-in trigger).
// `className` is tailwind-merged, so a `sm:max-w-*` replaces the default width
// rather than stacking. Under `DialogProvider` each reports close-completion so
// a lazily-mounted dialog unmounts only after its exit animation.
import {
  Dialog as DialogRoot,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@kubetail/ui/elements/dialog';
import { cn } from '@kubetail/ui/lib/utils';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

import { useDialogHost } from '@/lib/dialog';

// Restores window dragging while a dialog is open: the backdrop covers the
// shell's title-bar drag band, so this band must be portaled to `document.body`
// (the backdrop's stacking context) to sit above it. Height must match
// `app-bar/bar.tsx`'s bar (56px on every platform). A press here does not close
// the dialog: base-ui dismisses a modal dialog only on a press whose target is
// its backdrop.
// See docs/adr/2026-08-09-per-platform-window-chrome.md
function WindowDragBand() {
  return createPortal(
    <div
      data-testid="dialog-window-drag-region"
      data-tauri-drag-region
      aria-hidden
      className="fixed inset-x-0 top-0 z-60 h-14"
    />,
    document.body,
  );
}

type PanelDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Extra classes for the dialog content box — chiefly its size (e.g. `sm:max-w-4xl`).
  className?: string;
  children: ReactNode;
};

function Root({ open, onOpenChange, children }: Omit<PanelDialogProps, 'className'>) {
  const host = useDialogHost();
  return (
    <DialogRoot
      open={open}
      onOpenChange={(nextOpen) => onOpenChange(nextOpen)}
      // Close transition settled — the host may now unmount us.
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) host?.notifyClosed();
      }}
    >
      {open && <WindowDragBand />}
      {children}
    </DialogRoot>
  );
}

type DialogProps = PanelDialogProps & {
  title: ReactNode;
  description?: ReactNode;
};

export function Dialog({ open, onOpenChange, title, description, className, children }: DialogProps) {
  return (
    <Root open={open} onOpenChange={onOpenChange}>
      {/* Column + viewport cap so the body scrolls inside instead of pushing the
          dialog off-screen. */}
      <DialogContent className={cn('flex max-h-[85vh] flex-col', className)}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        {/* min-h-0: let this flex child shrink below its content so overflow
            scrolls here rather than growing the dialog. */}
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </DialogContent>
    </Root>
  );
}

// Children must draw a `DialogTitle`: it names the dialog.
export function PanelDialog({ open, onOpenChange, className, children }: PanelDialogProps) {
  return (
    <Root open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false} className={cn('flex max-h-[85vh] gap-0 overflow-hidden p-0', className)}>
        {children}
      </DialogContent>
    </Root>
  );
}

export { DialogClose, DialogDescription, DialogTitle };

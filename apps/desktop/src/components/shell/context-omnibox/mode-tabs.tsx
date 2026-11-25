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

// The omnibox's Chat/Dashboard tabs: a muted track holding two labelled tabs, the active
// one white. Router links, not local state, so each mode is a real deep-linkable route;
// the active one is styled off the router's `data-status`.
import { Tooltip, TooltipContent, TooltipTrigger } from '@kubetail/ui/elements/tooltip';
import { cn } from '@kubetail/ui/lib/utils';
import { Link } from '@tanstack/react-router';
import { LayoutDashboard, MessageCircle } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useRef, useState } from 'react';

const TAB_CLASS = cn(
  'flex h-7 flex-1 items-center justify-center gap-1.5 rounded-full py-0.75 pr-2.5 pl-2',
  'text-[13px] font-medium text-muted-foreground transition-colors hover:text-foreground',
  'data-[status=active]:bg-background data-[status=active]:text-foreground data-[status=active]:shadow-pill',
);

function Tab({ to, label, icon: Icon }: { to: '/chat' | '/dashboard'; label: string; icon: LucideIcon }) {
  // The tooltip says the label only while the label is hidden. Container queries are
  // CSS's alone, so ask the label whether it is drawn.
  const labelRef = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  const onOpenChange = (next: boolean) => setOpen(next && labelRef.current?.getClientRects().length === 0);

  return (
    <Tooltip open={open} onOpenChange={onOpenChange}>
      <TooltipTrigger render={<Link to={to} aria-label={label} className={TAB_CLASS} />}>
        <Icon className="size-3.75 shrink-0" aria-hidden />
        {/* Under 20rem (`xs`) of omnibox the tab is its icon alone. */}
        <span ref={labelRef} className="@max-xs:hidden">
          {label}
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

// The omnibox lays the track out and gives it its hit area.
export function ModeTabs({ className }: { className?: string }) {
  return (
    <nav aria-label="View" className={cn('flex h-8 items-center gap-0.5 rounded-full bg-muted p-0.5', className)}>
      <Tab to="/chat" label="Chat" icon={MessageCircle} />
      <Tab to="/dashboard" label="Dashboard" icon={LayoutDashboard} />
    </nav>
  );
}

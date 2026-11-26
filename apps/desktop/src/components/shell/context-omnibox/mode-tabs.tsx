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

// The omnibox's Chat/Dashboard tabs: a muted track holding two icon tabs, each named in its
// tooltip, the active one white. Router links, not local state, so each mode is a real
// deep-linkable route; the active one is styled off the router's `data-status`.
import { Tooltip, TooltipContent, TooltipTrigger } from '@kubetail/ui/elements/tooltip';
import { cn } from '@kubetail/ui/lib/utils';
import { Link } from '@tanstack/react-router';
import { LayoutDashboard, MessageCircle } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

import { APP_BAR_CAPSULE_CLASS } from '@/lib/control-style';

const TAB_CLASS = cn(
  'flex h-7 items-center justify-center rounded-full px-3',
  'text-muted-foreground transition-colors hover:text-foreground',
  'data-[status=active]:bg-background data-[status=active]:text-foreground data-[status=active]:shadow-pill',
);

function Tab({ to, label, icon: Icon }: { to: '/chat' | '/dashboard'; label: string; icon: LucideIcon }) {
  return (
    <Tooltip>
      <TooltipTrigger render={<Link to={to} aria-label={label} className={TAB_CLASS} />}>
        <Icon className="size-3.75" aria-hidden />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

// The omnibox lays the track out and gives it its hit area.
export function ModeTabs({ className }: { className?: string }) {
  return (
    <nav aria-label="View" className={cn(APP_BAR_CAPSULE_CLASS, className)}>
      <Tab to="/chat" label="Chat" icon={MessageCircle} />
      <Tab to="/dashboard" label="Dashboard" icon={LayoutDashboard} />
    </nav>
  );
}

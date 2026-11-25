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

// A group's header row in the dashboard's resource nav (the mockups' `K8s/NavGroup`): a
// toggle for the kinds under it, with the sum of their counts. `active` draws it as the
// current page while it is closed over the current kind, so the selection stays in view.
import { ChevronRight } from 'lucide-react';

import { COUNT, SELECTED } from './nav-item';

type NavGroupProps = {
  label: string;
  open: boolean;
  onToggle: () => void;
  count?: number;
  active?: boolean;
};

export function NavGroup({ label, open, onToggle, count, active = false }: NavGroupProps) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      data-active={active}
      className={`flex min-h-6 w-full items-center gap-1.5 rounded-md py-0.5 pr-2 pl-1 text-left text-[13px] font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent/50 ${SELECTED}`}
    >
      <ChevronRight className={`size-3.5 shrink-0 transition-transform ${open ? 'rotate-90' : ''}`} aria-hidden />
      <span className="truncate">{label}</span>
      {count !== undefined && <span className={COUNT}>{count.toLocaleString()}</span>}
    </button>
  );
}

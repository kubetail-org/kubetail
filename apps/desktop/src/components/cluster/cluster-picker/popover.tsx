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

// The cluster picker: the omnibox's popover listing the kubeconfig's contexts.
// A row switches the window's cluster; the footer opens the Clusters dialog.
import { Popover as PopoverPrimitive } from '@base-ui/react/popover';
import { Button } from '@kubetail/ui/elements/button';
import { PopoverClose } from '@kubetail/ui/elements/popover';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { X } from 'lucide-react';
import { useRef, useState } from 'react';
import type { ReactElement, ReactNode, RefObject } from 'react';

import { useActiveKubeContext } from '@/lib/active-kube-context';
import { useClusters } from '@/lib/clusters';
import { useDialog } from '@/lib/dialog';
import { useLogsView } from '@/lib/logs-view';

import { OmniboxPopover } from '../omnibox-popover';
import { ClusterList } from './list';

function Notice({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-1.5 px-3 py-2 text-sm text-muted-foreground">{children}</div>;
}

export function ClusterPicker({
  anchorRef,
  children,
}: {
  anchorRef: RefObject<HTMLElement | null>;
  children: ReactElement;
}) {
  const { context, contexts, phase } = useActiveKubeContext();
  const { clusters } = useClusters();
  const { clear: clearView } = useLogsView();
  const { openDialog } = useDialog();
  const navigate = useNavigate();
  // Subscribed via `useLocation` (re-renders per navigation): the bar stays mounted
  // across every navigation, and `useMatchRoute` would read stale until a reload.
  const pathname = useLocation({ select: (location) => location.pathname });
  const [open, setOpen] = useState(false);
  const filterRef = useRef<HTMLInputElement>(null);

  // Not `setContext`: a chat belongs to a cluster, so a switch leaves whichever chat
  // the window is on (the dashboard panel's `chat` param, and chat mode's open chat,
  // which is the route itself) and the log view that chat focused. The out-of-scope
  // notice is for arriving at such a chat (a deep link, Back), never for stepping off one.
  // A namespace belongs to a cluster too, so the scope goes back to every namespace.
  const openChat = pathname.startsWith('/chat/');
  const select = (name: string) => {
    setOpen(false);
    if (name === context) return;
    clearView();
    void navigate({
      to: openChat ? '/chat' : '.',
      search: (prev) => ({ ...prev, kubeContext: name, namespace: undefined, chat: undefined }),
    });
  };

  const manage = () => {
    setOpen(false);
    openDialog('clusters');
  };

  let body: ReactElement;
  if (contexts.length > 0) {
    body = (
      <ClusterList contexts={contexts} current={context} clusters={clusters} onSelect={select} filterRef={filterRef} />
    );
  } else if (phase === 'connecting') {
    body = (
      <Notice>
        <Spinner size="xs" className="mr-0" />
        Connecting…
      </Notice>
    );
  } else {
    body = <Notice>No kubeconfig</Notice>;
  }

  const popup = (
    <PopoverPrimitive.Popup
      data-testid="cluster-picker"
      // The filter when there is one; otherwise the popup's own default.
      initialFocus={() => filterRef.current ?? true}
      className="flex flex-col gap-3 rounded-xl border bg-popover p-[18px] text-popover-foreground shadow-lg outline-hidden"
      style={{
        width: 'max(var(--anchor-width), 640px)',
        maxWidth: 'var(--available-width)',
        maxHeight: 'var(--available-height)',
      }}
    >
      <div className="flex items-center gap-2">
        <p className="flex-1 text-base font-medium">Clusters</p>
        <PopoverClose
          render={<Button variant="ghost" size="icon-xs" className="text-muted-foreground" />}
          aria-label="Close"
        >
          <X aria-hidden />
        </PopoverClose>
      </div>
      {body}
      {/* The one way to the Clusters dialog, so a user with no kubeconfig still has it. */}
      <div className="border-t pt-2">
        <Button variant="link" size="xs" className="h-auto px-0" onClick={manage}>
          Manage clusters…
        </Button>
      </div>
    </PopoverPrimitive.Popup>
  );

  return (
    <OmniboxPopover anchorRef={anchorRef} popup={popup} open={open} onOpenChange={setOpen}>
      {children}
    </OmniboxPopover>
  );
}

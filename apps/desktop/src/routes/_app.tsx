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

// Pathless layout route: wraps chat/dashboard in `AppLayout` so they share the
// sidebar (a log window's page nests under `_logs` instead). Owns the
// window's search params — `kubeContext` and the dashboard's `resource`, `namespace`
// and `chat` — and `retainSearchParams` carries them across the mode switch, so coming
// back to the dashboard returns to the resource you left.
// See docs/adr/2026-08-09-url-params-as-window-state.md
import { createRoute, retainSearchParams } from '@tanstack/react-router';

import { AppLayout } from '@/layouts/app-layout';
import type { DashboardResource } from '@/lib/dashboard-resources';
import { Route as rootRoute } from '@/routes/__root';

type AppSearch = { kubeContext?: string; resource?: DashboardResource; namespace?: string; chat?: string };

export const Route = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  validateSearch: (search: Record<string, unknown>): AppSearch => ({
    ...(typeof search.kubeContext === 'string' ? { kubeContext: search.kubeContext } : {}),
    // The focused kind. Optional (absent resolves to the default without rewriting a
    // bare `/dashboard`) and validated leniently — it may name a dynamic kind, not a
    // closed union.
    ...(typeof search.resource === 'string' && search.resource.length > 0 ? { resource: search.resource } : {}),
    // The dashboard's namespace scope. Absent is every namespace.
    ...(typeof search.namespace === 'string' && search.namespace.length > 0 ? { namespace: search.namespace } : {}),
    // Which chat the dashboard's right-sidebar panel is on. Absent is its recent
    // list; it rides into /chat URLs, where nothing reads it.
    ...(typeof search.chat === 'string' && search.chat.length > 0 ? { chat: search.chat } : {}),
  }),
  search: { middlewares: [retainSearchParams(['kubeContext', 'resource', 'namespace', 'chat'])] },
  component: AppLayout,
});

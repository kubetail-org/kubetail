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

import { createRootRoute, Outlet } from '@tanstack/react-router';
import { lazy, Suspense } from 'react';
import { Provider as UrqlProvider } from 'urql';

import { WindowFrame } from '@/components/primitives/window-frame';
import { WindowResizeHandles } from '@/components/primitives/window-resize-handles';
import { ErrorBoundary } from '@/components/shell/error-boundary';
import { ReadyGate } from '@/components/shell/ready-gate';
import { AuthProvider } from '@/lib/auth';
import { ClustersProvider } from '@/lib/clusters';
import { createGraphqlClient } from '@/lib/graphql/client';
import { KubeConfigProvider } from '@/lib/kube-config';
import { ModelsProvider } from '@/lib/models';
import { ThemeProvider } from '@/lib/theme';

const TanStackRouterDevtools =
  import.meta.env.VITE_ROUTER_DEVTOOLS === 'true'
    ? /* v8 ignore start -- a dev-run switch (docs/development-switches.md) the suite runs without */
      lazy(() =>
        import('@tanstack/react-router-devtools').then((res) => ({
          default: res.TanStackRouterDevtools,
        })),
      )
    : /* v8 ignore stop */ () => null;

// One client for the app's lifetime; module-load creation gives routeTree-mounted
// tests the Provider transitively.
const gqlClient = createGraphqlClient();

export const Route = createRootRoute({
  notFoundComponent: NotFound,
  component: RootComponent,
});

export function NotFound() {
  return (
    <div className="container">
      <h1>404</h1>
      <p>Page not found.</p>
    </div>
  );
}

// Providers only — the visual shell lives in the `_app` layout route (`AppLayout`).
// `WindowFrame` wraps everything, loading/error states included.
// `WindowResizeHandles` must stay a sibling, not a child: `WindowFrame`'s
// `contain: paint` would re-anchor and clip its fixed grips.
// See docs/adr/2026-08-09-per-platform-window-chrome.md
function RootComponent() {
  return (
    <>
      <WindowFrame>
        <ThemeProvider>
          <ErrorBoundary>
            <ReadyGate>
              <UrqlProvider value={gqlClient}>
                <AuthProvider>
                  <ClustersProvider>
                    <ModelsProvider>
                      <KubeConfigProvider>
                        <Outlet />
                        <Suspense>
                          <TanStackRouterDevtools />
                        </Suspense>
                      </KubeConfigProvider>
                    </ModelsProvider>
                  </ClustersProvider>
                </AuthProvider>
              </UrqlProvider>
            </ReadyGate>
          </ErrorBoundary>
        </ThemeProvider>
      </WindowFrame>
      <WindowResizeHandles />
    </>
  );
}

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

// `/` owns no view — it redirects to `DEFAULT_ROUTE`, the one place the default
// lives (New Window / secondary windows open at `/`).
import { createRoute, redirect } from '@tanstack/react-router';

import { Route as appRoute } from '@/routes/_app';

/** The view `/` lands on. Flip this to change the app's default. */
export const DEFAULT_ROUTE = '/chat';

export const Route = createRoute({
  getParentRoute: () => appRoute,
  path: '/',
  beforeLoad: () => {
    // oxlint-disable-next-line typescript/only-throw-error -- TanStack signals a redirect by throwing the `redirect()` result
    throw redirect({ to: DEFAULT_ROUTE });
  },
});

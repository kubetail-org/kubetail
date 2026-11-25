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

import { Route as rootRoute } from '@/routes/__root';
import { Route as appRoute } from '@/routes/_app';
import { Route as logsRoute } from '@/routes/_logs';
import { Route as chatRoute } from '@/routes/chat';
import { Route as chatIdRoute } from '@/routes/chat-id';
import { Route as dashboardRoute } from '@/routes/dashboard';
import { Route as indexRoute } from '@/routes/index';
import { Route as logsWindowRoute } from '@/routes/logs-window';

// Chat and Dashboard are peers under the pathless `_app` layout route so they
// share the sidebar shell; `/` only redirects to the default view. A chat is a
// route of its own (`/chat/$chatId`) so each window can be in a different one. A log
// window's page sits under its own pathless layout, `_logs`.
export const routeTree = rootRoute.addChildren([
  appRoute.addChildren([indexRoute, chatRoute, chatIdRoute, dashboardRoute]),
  logsRoute.addChildren([logsWindowRoute]),
]);

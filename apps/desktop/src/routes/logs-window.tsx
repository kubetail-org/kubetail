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

// A log window's page, the path the host loads into every `logs-N` window.
import { createRoute } from '@tanstack/react-router';

import { LogsWindow } from '@/components/logs/log-window';
import { Route as logsRoute } from '@/routes/_logs';

export const Route = createRoute({
  getParentRoute: () => logsRoute,
  path: '/logs',
  component: LogsWindow,
});

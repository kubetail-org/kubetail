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

// Pathless layout route for a log window (`logs-N`): `LogsWindowLayout` around its
// page. It owns no search params; the page asks the host what to draw.
import { createRoute } from '@tanstack/react-router';

import { LogsWindowLayout } from '@/layouts/logs-window-layout';
import { Route as rootRoute } from '@/routes/__root';

export const Route = createRoute({
  getParentRoute: () => rootRoute,
  id: 'logs-window',
  component: LogsWindowLayout,
});

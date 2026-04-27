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

import { ApolloProvider } from '@apollo/client/react';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { createBrowserRouter, createRoutesFromElements, RouterProvider } from 'react-router-dom';

import { dashboardClient } from '@/apollo-client';
import { SessionProvider } from '@/lib/auth';
import { PreferencesProvider } from '@/lib/preferences';
import { ThemeEffect } from '@/lib/theme';
import { UpdateNotificationProvider } from '@/lib/update-notifications';
import { getBasename } from '@/lib/util';
import { routes } from './routes';

import './index.css';
import 'unfonts.css';

const router = createBrowserRouter(createRoutesFromElements(routes), { basename: getBasename() });

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <SessionProvider>
      <ApolloProvider client={dashboardClient}>
        <PreferencesProvider>
          <ThemeEffect />
          <UpdateNotificationProvider>
            <RouterProvider router={router} />
          </UpdateNotificationProvider>
        </PreferencesProvider>
      </ApolloProvider>
    </SessionProvider>
  </StrictMode>,
);

// Copyright 2024 Andres Morey
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

import { ApolloProvider } from '@apollo/client';
import React from 'react';
import ReactDOM from 'react-dom/client';
import { createBrowserRouter, createRoutesFromElements, RouterProvider } from 'react-router-dom';

import { routes } from './routes';
import client from '@/apollo-client';
import { SessionProvider } from '@/lib/auth';
import { getBasename } from '@/lib/helpers';
import { ThemeProvider } from '@/lib/theme';

import './index.css';

// handle runtimeConfig (inserted by server in production)
if ('runtimeConfig' in window) {
  // @ts-expect-error 'window.runtimeConfig' is of type 'unknown'
  import.meta.env.VITE_HELLO = window.runtimeConfig.basePath;
  console.log(import.meta.env.VITE_HELLO);
}

const router = createBrowserRouter(createRoutesFromElements(routes), { basename: getBasename() });

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ApolloProvider client={client}>
      <SessionProvider>
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>
      </SessionProvider>
    </ApolloProvider>
  </React.StrictMode>,
);

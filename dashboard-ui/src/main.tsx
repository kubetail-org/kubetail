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
import React, { Suspense } from 'react';
import ReactDOM from 'react-dom/client';
import { ErrorBoundary } from 'react-error-boundary';
import { createBrowserRouter, createRoutesFromElements, RouterProvider } from 'react-router-dom';

import Spinner from '@kubetail/ui/elements/Spinner';

import { routes } from './routes';
import client from '@/apollo-client';
import { SessionProvider } from '@/lib/auth';
import { getBasename } from '@/lib/helpers';
import { ThemeProvider } from '@/lib/theme';

import './index.css';

const router = createBrowserRouter(createRoutesFromElements(routes), { basename: getBasename() });

function fallbackRenderError({ error }: { error: Error }) {
  return (
    <div role="alert">
      <p>Something went wrong:</p>
      <pre className="text-red-500">{error.message}</pre>
    </div>
  );
}

const LoadingModal = () => (
  <div className="relative z-10" role="dialog">
    <div className="fixed inset-0 bg-chrome-500 bg-opacity-75" />
    <div className="fixed inset-0 z-10 w-screen">
      <div className="flex min-h-full items-center justify-center p-0 text-center">
        <div className="relative transform overflow-hidden rounded-lg bg-background my-8 p-6 text-left shadow-xl">
          <div className="flex items-center space-x-2">
            <div>Connecting...</div>
            <Spinner size="sm" />
          </div>
        </div>
      </div>
    </div>
  </div>
);

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ApolloProvider client={client}>
      <SessionProvider>
        <ThemeProvider>
          <ErrorBoundary fallbackRender={fallbackRenderError}>
            <Suspense fallback={<LoadingModal />}>
              <RouterProvider router={router} />
            </Suspense>
          </ErrorBoundary>
        </ThemeProvider>
      </SessionProvider>
    </ApolloProvider>
  </React.StrictMode>,
);

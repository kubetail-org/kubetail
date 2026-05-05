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

import { MockedProvider } from '@apollo/client/testing/react';
import type { MockedResponse } from '@apollo/client/testing';
import { render } from '@testing-library/react';
import { createStore, Provider as JotaiProvider } from 'jotai';
import React from 'react';
import { MemoryRouter } from 'react-router-dom';

import { DashboardCustomCache } from '@/apollo-client';
import appConfig from '@/app-config';
import { KubeConfigEffect } from '@/lib/kubeconfig';

export const renderElement = (component: React.ReactElement, mocks?: MockedResponse[]) =>
  render(
    <JotaiProvider store={createStore()}>
      <MockedProvider
        mocks={mocks}
        cache={new DashboardCustomCache()}
        defaultOptions={{
          watchQuery: { fetchPolicy: 'cache-first' },
        }}
      >
        <MemoryRouter>
          <KubeConfigEffect />
          {component}
        </MemoryRouter>
      </MockedProvider>
    </JotaiProvider>,
  );

/**
 * Run `fn` with the given appConfig overrides applied, then restore the previous values — even if
 * `fn` throws. Lets tests exercise non-default config (e.g. `environment: 'cluster'`) without
 * leaving the global `appConfig` mutated for any later test or test file.
 */
export async function withAppConfig<T>(overrides: Partial<typeof appConfig>, fn: () => Promise<T> | T): Promise<T> {
  const original: Partial<typeof appConfig> = {};
  const keys = Object.keys(overrides) as (keyof typeof appConfig)[];
  keys.forEach((key) => {
    original[key] = appConfig[key] as never;
    Object.defineProperty(appConfig, key, { value: overrides[key], writable: true, configurable: true });
  });
  try {
    return await fn();
  } finally {
    keys.forEach((key) => {
      Object.defineProperty(appConfig, key, { value: original[key], writable: true, configurable: true });
    });
  }
}

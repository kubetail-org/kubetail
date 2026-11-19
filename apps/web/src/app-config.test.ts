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

import { Config } from '@/app-config';

describe('Config', () => {
  it('uses the defaults when nothing overrides them', () => {
    const config = new Config({}, {});
    expect(config.clusterAPIEnabled).toBe(true);
    expect(config.columns).toEqual(['timestamp', 'dot']);
    expect(config.environment).toBe('desktop');
  });

  it('parses VITE_CLUSTER_API_ENABLED=false to false', () => {
    const config = new Config({}, { VITE_CLUSTER_API_ENABLED: 'false' });
    expect(config.clusterAPIEnabled).toBe(false);
  });

  it('parses VITE_CLUSTER_API_ENABLED=true to true', () => {
    const config = new Config({}, { VITE_CLUSTER_API_ENABLED: 'true' });
    expect(config.clusterAPIEnabled).toBe(true);
  });

  it('parses VITE_COLUMNS as a comma-separated list', () => {
    const config = new Config({}, { VITE_COLUMNS: 'timestamp, pod,,container' });
    expect(config.columns).toEqual(['timestamp', 'pod', 'container']);
  });

  it('reads a string key from its env variable', () => {
    const config = new Config({}, { VITE_AUTH_MODE: 'token', VITE_BASE_PATH: '/kubetail/' });
    expect(config.authMode).toBe('token');
    expect(config.basePath).toBe('/kubetail/');
  });

  it('prefers runtimeConfig over env and keeps its types', () => {
    const config = new Config(
      { clusterAPIEnabled: false, columns: ['message'] },
      { VITE_CLUSTER_API_ENABLED: 'true', VITE_COLUMNS: 'timestamp' },
    );
    expect(config.clusterAPIEnabled).toBe(false);
    expect(config.columns).toEqual(['message']);
  });

  it('reads window.runtimeConfig by default', () => {
    Object.assign(window, { runtimeConfig: { environment: 'cluster' } });
    try {
      expect(new Config().environment).toBe('cluster');
    } finally {
      Reflect.deleteProperty(window, 'runtimeConfig');
    }
  });
});

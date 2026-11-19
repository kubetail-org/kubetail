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

/**
 * The `import.meta.env` variable that overrides each key
 */

const ENV_KEYS = {
  authMode: 'VITE_AUTH_MODE',
  basePath: 'VITE_BASE_PATH',
  cliVersion: 'VITE_CLI_VERSION',
  clusterAPIEnabled: 'VITE_CLUSTER_API_ENABLED',
  columns: 'VITE_COLUMNS',
  environment: 'VITE_ENVIRONMENT',
} as const;

/**
 * Parse an environment variable to the type of the key's default
 */

function parseEnvValue(value: string, defaultValue: unknown) {
  if (typeof defaultValue === 'boolean') return value === 'true';
  if (Array.isArray(defaultValue)) {
    return value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }
  return value;
}

/**
 * Config
 */

export class Config {
  authMode = 'auto';

  basePath = '/';

  cliVersion = '';

  clusterAPIEnabled = true;

  columns: string[] = ['timestamp', 'dot'];

  environment = 'desktop';

  // runtimeConfig is JSON the server writes into the page, so its values already have their types.
  // Environment variables are strings, parsed to each key's type.
  constructor(
    runtimeConfig: Record<string, unknown> = 'runtimeConfig' in window
      ? (window.runtimeConfig as Record<string, unknown>)
      : {},
    env: Record<string, unknown> = import.meta.env,
  ) {
    (Object.keys(ENV_KEYS) as (keyof typeof ENV_KEYS)[]).forEach((key) => {
      if (key in runtimeConfig) {
        Object.assign(this, { [key]: runtimeConfig[key] });
        return;
      }

      const envValue = env[ENV_KEYS[key]];
      if (typeof envValue === 'string') {
        Object.assign(this, { [key]: parseEnvValue(envValue, this[key]) });
      }
    });
  }
}

export const config = new Config();
export default config;

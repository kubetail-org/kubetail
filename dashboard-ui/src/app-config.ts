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
 * camelCaseToUpperCaseWithUnderscores
 */

function camelCaseToUpperCaseWithUnderscores(input: string) {
  return input.replace(/([A-Z])/g, '_$1').toUpperCase();
}

/**
 * Config
 */

class Config {
  authMode = 'auto';

  basePath = '/';

  clusterAPIEnabled = true;

  clusterAPIEndpoint = '';

  environment = 'desktop';

  constructor() {
    const runtimeConfig = ('runtimeConfig' in window ? window.runtimeConfig : {}) as Record<string, string>;

    // override defaults with runtimeConfig or import.meta.env
    Object.keys(this).forEach((key) => {
      const configKey = key as keyof this;

      // 1 - check runtime config
      if (key in runtimeConfig) {
        // @ts-expect-error Type 'string' is not assignable to type 'this[Extract<keyof this, string>]'.
        this[configKey] = runtimeConfig[key];
        return;
      }

      // 2 - check import.meta.env
      const envKey = `VITE_${camelCaseToUpperCaseWithUnderscores(key)}`;
      if (envKey in import.meta.env) {
        this[configKey] = import.meta.env[envKey];
      }
    });
  }
}

export const config = new Config();
export default config;

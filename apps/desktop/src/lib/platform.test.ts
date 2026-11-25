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

import { afterEach, describe, expect, it } from 'vitest';

import { MAC_USER_AGENT, NON_MAC_USER_AGENT, WINDOWS_USER_AGENT, restoreUserAgent, setUserAgent } from '@/test-utils';

import { isLinux, isMacOS, isWindows } from './platform';

// `NON_MAC_USER_AGENT` is a Linux UA (see `test-utils`).
afterEach(restoreUserAgent);

describe('isMacOS', () => {
  it('is true on a macOS webview user agent', () => {
    setUserAgent(MAC_USER_AGENT);
    expect(isMacOS()).toBe(true);
  });

  it('is false on Windows', () => {
    setUserAgent(WINDOWS_USER_AGENT);
    expect(isMacOS()).toBe(false);
  });

  it('is false on Linux', () => {
    setUserAgent(NON_MAC_USER_AGENT);
    expect(isMacOS()).toBe(false);
  });
});

describe('isLinux', () => {
  it('is true on a Linux webview user agent', () => {
    setUserAgent(NON_MAC_USER_AGENT);
    expect(isLinux()).toBe(true);
  });

  it('is false on macOS', () => {
    setUserAgent(MAC_USER_AGENT);
    expect(isLinux()).toBe(false);
  });

  it('is false on Windows', () => {
    setUserAgent(WINDOWS_USER_AGENT);
    expect(isLinux()).toBe(false);
  });
});

describe('isWindows', () => {
  it('is true on a Windows webview user agent', () => {
    setUserAgent(WINDOWS_USER_AGENT);
    expect(isWindows()).toBe(true);
  });

  it('is false on macOS', () => {
    setUserAgent(MAC_USER_AGENT);
    expect(isWindows()).toBe(false);
  });

  it('is false on Linux', () => {
    setUserAgent(NON_MAC_USER_AGENT);
    expect(isWindows()).toBe(false);
  });
});

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

import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { reportError } from '@/lib/error-bus';

import { ConnectionStatus } from './connection-status';

// Helpers -------------------------------------------------------------

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

const banner = () => screen.queryByRole('status');

// The banner clears itself on a timer; no wall-clock waiting.
const advance = (ms: number) =>
  act(() => {
    vi.advanceTimersByTime(ms);
  });

const report = (error: Parameters<typeof reportError>[0]) => act(() => reportError(error));

// Tests ---------------------------------------------------------------

describe('ConnectionStatus', () => {
  it('shows nothing until something fails', () => {
    render(<ConnectionStatus />);
    expect(banner()).not.toBeInTheDocument();
  });

  it('names the failure for an ordinary error', () => {
    render(<ConnectionStatus />);
    report({ source: 'graphql', message: 'sidecar unreachable' });
    expect(banner()).toHaveTextContent('Error: sidecar unreachable');
  });

  it('softens the wording for a dropped subscription, which reconnects on its own', () => {
    render(<ConnectionStatus />);
    report({ source: 'subscription', message: 'watch failed' });
    expect(banner()).toHaveTextContent('Subscription dropped — reconnecting…');
  });

  it('dismisses itself, so a brief outage leaves nothing stale', () => {
    render(<ConnectionStatus />);
    report({ source: 'network', message: 'boom' });

    advance(4_999);
    expect(banner()).toBeInTheDocument();
    advance(1);
    expect(banner()).not.toBeInTheDocument();
  });

  it('restarts the countdown on a second failure', () => {
    render(<ConnectionStatus />);
    report({ source: 'network', message: 'first' });
    advance(4_000);

    report({ source: 'network', message: 'second' });
    // The first error's deadline passes; the second one's has not.
    advance(1_000);
    expect(banner()).toHaveTextContent('Error: second');

    advance(4_000);
    expect(banner()).not.toBeInTheDocument();
  });

  it('stops listening once unmounted', () => {
    const { unmount } = render(<ConnectionStatus />);
    unmount();
    // Reporting into a bus the banner has left must not reach it (React would warn
    // about a state update on an unmounted component).
    report({ source: 'network', message: 'after unmount' });
    expect(banner()).not.toBeInTheDocument();
  });
});

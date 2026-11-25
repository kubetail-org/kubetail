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

import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { AppError } from '@/lib/error-bus';

const reportError = vi.fn<(e: AppError) => void>();
vi.mock('@/lib/error-bus', () => ({ reportError: (e: AppError) => reportError(e) }));

const { ErrorBoundary } = await import('./error-boundary');

function Boom({ message = 'kaboom' }: { message?: string }): never {
  throw new Error(message);
}

describe('ErrorBoundary', () => {
  // React logs caught render errors to console.error; componentDidCatch logs
  // too. Silence both so the suite output stays readable.
  beforeEach(() => {
    reportError.mockReset();
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('renders children unchanged when nothing throws', () => {
    render(
      <ErrorBoundary>
        <p>all good</p>
      </ErrorBoundary>,
    );
    expect(screen.getByText('all good')).toBeInTheDocument();
    expect(reportError).not.toHaveBeenCalled();
  });

  it('renders the fallback with the error message when a child throws', () => {
    render(
      <ErrorBoundary>
        <Boom message="render exploded" />
      </ErrorBoundary>,
    );
    expect(screen.getByRole('heading', { name: 'Something went wrong' })).toBeInTheDocument();
    expect(screen.getByText('render exploded')).toBeInTheDocument();
  });

  it('mirrors the caught error onto the bus as a render error', () => {
    render(
      <ErrorBoundary>
        <Boom message="bus me" />
      </ErrorBoundary>,
    );
    expect(reportError).toHaveBeenCalledTimes(1);
    const arg = reportError.mock.calls[0][0];
    expect(arg.source).toBe('render');
    expect(arg.message).toBe('bus me');
    expect(arg.cause).toBeInstanceOf(Error);
    expect((arg.cause as Error).message).toBe('bus me');
  });

  it('carries the component stack on the bus record', () => {
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );
    const arg = reportError.mock.calls[0][0];
    expect(arg.componentStack).toContain('Boom');
  });

  it('reloads the window when the reload button is clicked', async () => {
    const reload = vi.fn();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { reload },
    });

    const user = userEvent.setup();
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );

    await user.click(screen.getByRole('button', { name: 'Reload window' }));
    expect(reload).toHaveBeenCalledTimes(1);
  });
});

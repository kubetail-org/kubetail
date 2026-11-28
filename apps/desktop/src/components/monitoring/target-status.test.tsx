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

import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { monitorTargetRecord } from '@/test-utils';

const { openDialog } = vi.hoisted(() => ({ openDialog: vi.fn() }));
vi.mock('@/lib/dialog', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/dialog')>()),
  useDialog: () => ({ openDialog }),
}));
vi.mock('@/lib/incidents', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/incidents')>()),
  useMonitorSignals: () => new Map([['kubernetes.crash_loop', { name: 'kubernetes.crash_loop', label: 'Crash loop' }]]),
}));

const { MonitorTargetStatus } = await import('./target-status');

beforeEach(() => vi.clearAllMocks());

describe('MonitorTargetStatus', () => {
  it('says monitoring is off, and nothing of the last reading, with a way to the Monitoring tab', async () => {
    render(<MonitorTargetStatus target={monitorTargetRecord({ enabled: false, readState: 'Failed' })} />);
    expect(screen.getByText(/Monitoring is off for/)).toHaveTextContent('Monitoring is off for prod.');
    expect(screen.queryByText(/cannot be read/)).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Monitoring settings' }));
    expect(openDialog).toHaveBeenCalledWith('settings', 'Monitoring');
  });

  it('says why a target cannot be read', () => {
    const target = monitorTargetRecord({
      readState: 'Failed',
      unknownSignals: [
        { name: 'kubernetes.crash_loop', reason: 'PermissionDenied' },
        { name: 'kubernetes.image_pull', reason: 'Unavailable' },
      ],
    });
    render(<MonitorTargetStatus target={target} />);
    expect(screen.getByText(/cannot be read/)).toHaveTextContent('prod cannot be read: access denied, unreachable.');
  });

  it('says a target has not been read yet', () => {
    render(<MonitorTargetStatus target={monitorTargetRecord({ readState: 'NeverRead' })} />);
    expect(screen.getByText(/has not been read yet/)).toBeInTheDocument();
  });

  it('names each signal a partial reading could not judge, and why, by its label', () => {
    const target = monitorTargetRecord({
      readState: 'Partial',
      unknownSignals: [
        { name: 'kubernetes.crash_loop', reason: 'NotReady' },
        { name: 'kubernetes.retired', reason: 'Truncated' },
      ],
    });
    render(<MonitorTargetStatus target={target} />);
    expect(screen.getByText('Crash loop: still loading')).toBeInTheDocument();
    expect(screen.getByText('kubernetes.retired: answer cut short')).toBeInTheDocument();
  });

  it('draws nothing for a target that is on and judged everything', () => {
    const { container } = render(<MonitorTargetStatus target={monitorTargetRecord()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("draws the target's label as visible text", () => {
    render(<MonitorTargetStatus target={monitorTargetRecord({ readState: 'NeverRead', label: 'pr‮od' })} />);
    expect(screen.getByText('\\u{202E}')).toBeInTheDocument();
  });
});

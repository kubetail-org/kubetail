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

import { fireEvent, render, screen } from '@testing-library/react';

import appConfig from '@/app-config';

import UpgradeBanner from './UpgradeBanner';

const cliStatus = { currentVersion: '0.9.0', latestVersion: '1.0.0', updateAvailable: true };
const clusterStatus = { currentVersion: '0.8.0', latestVersion: '0.9.0', updateAvailable: true };
const noUpdate = { currentVersion: '1.0.0', latestVersion: '1.0.0', updateAvailable: false };

beforeEach(() => {
  Object.defineProperty(appConfig, 'environment', { value: 'desktop', writable: true });
});

describe('UpgradeBanner', () => {
  it('renders CLI and cluster update messages', () => {
    render(
      <UpgradeBanner cliStatus={cliStatus} clusterStatus={clusterStatus} dismiss={vi.fn()} dontRemindMe={vi.fn()} />,
    );

    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.getByText(/CLI 0\.9\.0/)).toBeInTheDocument();
    expect(screen.getByText(/Helm chart 0\.8\.0/)).toBeInTheDocument();
  });

  it('renders only cluster update in cluster mode', () => {
    Object.defineProperty(appConfig, 'environment', { value: 'cluster', writable: true });

    render(
      <UpgradeBanner cliStatus={cliStatus} clusterStatus={clusterStatus} dismiss={vi.fn()} dontRemindMe={vi.fn()} />,
    );

    expect(screen.getByText(/Helm chart/)).toBeInTheDocument();
    expect(screen.queryByText(/CLI/)).not.toBeInTheDocument();
  });

  it('returns null when no updates are available', () => {
    const { container } = render(
      <UpgradeBanner cliStatus={noUpdate} clusterStatus={noUpdate} dismiss={vi.fn()} dontRemindMe={vi.fn()} />,
    );

    expect(container.firstChild).toBeNull();
  });

  it('returns null when statuses are null', () => {
    const { container } = render(
      <UpgradeBanner cliStatus={null} clusterStatus={null} dismiss={vi.fn()} dontRemindMe={vi.fn()} />,
    );

    expect(container.firstChild).toBeNull();
  });

  it('calls dismiss when dismiss button is clicked', () => {
    const dismiss = vi.fn();

    render(
      <UpgradeBanner cliStatus={cliStatus} clusterStatus={clusterStatus} dismiss={dismiss} dontRemindMe={vi.fn()} />,
    );

    fireEvent.click(screen.getByLabelText('Dismiss'));
    expect(dismiss).toHaveBeenCalledOnce();
  });

  it('calls dontRemindMe when "Don\'t remind me" is clicked', () => {
    const dontRemindMe = vi.fn();

    render(
      <UpgradeBanner
        cliStatus={cliStatus}
        clusterStatus={clusterStatus}
        dismiss={vi.fn()}
        dontRemindMe={dontRemindMe}
      />,
    );

    fireEvent.click(screen.getByText("Don't remind me"));
    expect(dontRemindMe).toHaveBeenCalledOnce();
  });
});

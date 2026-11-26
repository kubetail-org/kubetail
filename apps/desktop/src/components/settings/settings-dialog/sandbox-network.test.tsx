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
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { PermissionTarget } from '@/gql/graphql';

const { cloud } = vi.hoisted(() => ({
  cloud: { current: { platforms: undefined as PermissionTarget[] | undefined, readFailed: false } },
}));
vi.mock('@/lib/sandbox-cloud-access', () => ({ useSandboxCloudAccess: () => cloud.current }));

const { SandboxNetwork } = await import('./sandbox-network');

const row = (label: string) => screen.getByText(label).closest('li')!;

beforeEach(() => {
  cloud.current = { platforms: undefined, readFailed: false };
});

describe('SandboxNetwork', () => {
  it('says how a command reaches your clusters', () => {
    render(<SandboxNetwork networkAvailable networkReason="" />);
    expect(screen.getByRole('group', { name: 'Network' })).toHaveTextContent('What a sandboxed command can reach.');
    expect(row('Your clusters')).toHaveTextContent('Through Kubetail, under your Permissions settings.');
  });

  it('names the cloud platforms turned on, or says none is', () => {
    cloud.current = { platforms: ['GitHub', 'AWS', 'GCP'], readFailed: false };
    const { unmount } = render(<SandboxNetwork networkAvailable networkReason="" />);
    expect(row('Cloud platforms')).toHaveTextContent(
      'GitHub, AWS, and Google Cloud, through Kubetail. Your sign-in stays outside.',
    );
    unmount();

    cloud.current = { platforms: [], readFailed: false };
    render(<SandboxNetwork networkAvailable networkReason="" />);
    expect(row('Cloud platforms')).toHaveTextContent('None. Every cloud platform is off.');
  });

  it('says when the cloud platforms could not be read, apart from every one being off', () => {
    cloud.current = { platforms: undefined, readFailed: true };
    render(<SandboxNetwork networkAvailable networkReason="" />);
    expect(row('Cloud platforms')).toHaveTextContent("Couldn't load cloud access.");
    expect(row('Cloud platforms')).not.toHaveTextContent('None.');
  });

  it('draws nothing in the row until the sidecar answers', () => {
    render(<SandboxNetwork networkAvailable networkReason="" />);
    expect(row('Cloud platforms')).toHaveTextContent(/^Cloud platforms$/);
  });

  it("says the internet is off by default, or why it can't be given", () => {
    const { unmount } = render(<SandboxNetwork networkAvailable networkReason="" />);
    expect(row('Everything else')).toHaveTextContent(
      'Off by default. You can allow the internet for a chat or a command.',
    );
    unmount();

    render(<SandboxNetwork networkAvailable={false} networkReason="pasta not found " />);
    expect(screen.getByTestId('network-reason').textContent).toBe('pasta not found\\u{20}');
  });
});

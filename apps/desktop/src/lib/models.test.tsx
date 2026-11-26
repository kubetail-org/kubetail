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

import { fireEvent, render, renderHook, screen } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Model } from './models';

// The watch's seam: the frames it has delivered, folded by the provider's own
// reducer, and how many times a component ran it.
const { watch } = vi.hoisted(() => ({
  watch: {} as { frames?: unknown[]; options?: unknown; opened: number },
}));
vi.mock('@/lib/graphql/use-folded-subscription', () => ({
  perFrame:
    <D, R>(step: (prev: R | undefined, frame: D) => R) =>
    (prev: R | undefined, frames: D[]) =>
      frames.reduce<R | undefined>(step, prev),
  useFoldedSubscription: (_args: unknown, reduce: (prev: unknown, frames: unknown[]) => unknown, options: unknown) => {
    watch.options = options;
    watch.opened += 1;
    return { data: watch.frames ? reduce(undefined, watch.frames) : undefined, connected: true };
  },
}));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { ModelsProvider, modelOf, providersOf, seedPick, useModels } = await import('./models');

const wrapper = ({ children }: { children: ReactNode }) => <ModelsProvider>{children}</ModelsProvider>;

const opus: Model = {
  provider: { id: 'anthropic', label: 'Anthropic' },
  id: 'claude-opus-5',
  label: 'Opus 5',
  efforts: ['low', 'high'],
  defaultEffort: 'high',
};
const fake: Model = {
  provider: { id: 'fake', label: 'Fake' },
  id: 'fake',
  label: 'Fake model',
  efforts: [],
  defaultEffort: '',
};

beforeEach(() => {
  watch.frames = undefined;
  watch.options = undefined;
  watch.opened = 0;
});

describe('useModels', () => {
  it('has answered nothing before the first frame', () => {
    const { result } = renderHook(() => useModels(), { wrapper });
    expect(result.current).toEqual({ models: [], snapshotComplete: false });
  });

  it('serves the catalog in the order the sidecar sent it', () => {
    watch.frames = [{ modelsStateWatch: [opus, fake] }];
    const { result } = renderHook(() => useModels(), { wrapper });
    expect(result.current.snapshotComplete).toBe(true);
    expect(result.current.models.map((m) => m.id)).toEqual(['claude-opus-5', 'fake']);
  });

  // A sidecar with no provider answers with nothing, which is a different thing
  // from not having answered: one disables Send with a placeholder, the other waits.
  it('has answered an empty catalog', () => {
    watch.frames = [{ modelsStateWatch: [] }];
    const { result } = renderHook(() => useModels(), { wrapper });
    expect(result.current).toEqual({ models: [], snapshotComplete: true });
  });

  // A connection that comes or goes changes the list: the latest frame is the
  // catalog.
  it('follows the latest frame', () => {
    watch.frames = [{ modelsStateWatch: [opus] }, { modelsStateWatch: [opus, fake] }];
    const { result } = renderHook(() => useModels(), { wrapper });
    expect(result.current.models).toEqual([opus, fake]);
  });

  // The provider holds the window's one stream, shared by every reader.
  it('opens one shared stream however many read it', () => {
    renderHook(() => [useModels(), useModels(), useModels()], { wrapper });
    expect(watch.opened).toBe(1);
    expect(watch.options).toBeUndefined();
  });

  // A state watch's first frame is its snapshot: a reader mounting after it reads the
  // provider's catalog rather than waiting on a frame that never comes again.
  it('serves the catalog to a reader that mounts after the first frame', async () => {
    watch.frames = [{ modelsStateWatch: [opus] }];
    function Reader() {
      return <p>{useModels().models.map((m) => m.label)}</p>;
    }
    function Pane() {
      const [open, setOpen] = useState(false);
      return open ? (
        <Reader />
      ) : (
        <button type="button" onClick={() => setOpen(true)}>
          Open
        </button>
      );
    }
    render(<Pane />, { wrapper });
    fireEvent.click(screen.getByRole('button', { name: 'Open' }));
    expect(await screen.findByText('Opus 5')).toBeInTheDocument();
    expect(watch.opened).toBe(1);
  });

  it('refuses a reader outside the provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => renderHook(() => useModels())).toThrow('useModels must be used inside <ModelsProvider>');
    spy.mockRestore();
  });
});

describe('seedPick', () => {
  it("starts on the chat's last answer, at the effort it ran", () => {
    expect(seedPick([opus, fake], { providerID: 'anthropic', id: 'claude-opus-5', effort: 'low' })).toEqual({
      model: { providerID: 'anthropic', id: 'claude-opus-5' },
      effort: 'low',
    });
  });

  it("starts on the catalog's first model when there is no answer to follow", () => {
    expect(seedPick([opus, fake], null)).toEqual({
      model: { providerID: 'anthropic', id: 'claude-opus-5' },
      effort: 'high',
    });
  });

  // The levels are the provider's own words, so the same name on another model is
  // not the same level — and inheriting it would quietly change what a turn costs.
  it("takes the fallback model's own default when the answer's model is gone", () => {
    const cheap = { ...opus, id: 'claude-sonnet-5', defaultEffort: 'low' };
    expect(seedPick([cheap, fake], { providerID: 'anthropic', id: 'claude-opus-5', effort: 'high' })).toEqual({
      model: { providerID: 'anthropic', id: 'claude-sonnet-5' },
      effort: 'low',
    });
  });

  // A disconnected ChatGPT plan lists nothing: the chat stays on it, unavailable,
  // rather than moving to another provider unasked.
  it('keeps the answer whose provider lists nothing', () => {
    expect(seedPick([opus], { providerID: 'chatgpt', id: 'gpt-6.1-sol', effort: 'medium' })).toEqual({
      model: { providerID: 'chatgpt', id: 'gpt-6.1-sol' },
      effort: 'medium',
    });
  });

  it("takes the model's default when the effort it ran is gone", () => {
    expect(seedPick([opus], { providerID: 'anthropic', id: 'claude-opus-5', effort: 'xhigh' })?.effort).toBe('high');
  });

  it('has nothing to pick from in an empty catalog', () => {
    expect(seedPick([], null)).toBeUndefined();
  });
});

describe('modelOf', () => {
  it('finds a model by provider and id', () => {
    expect(modelOf([opus, fake], { providerID: 'fake', id: 'fake' })).toBe(fake);
  });

  it('is null for a model the catalog no longer offers', () => {
    expect(modelOf([opus], { providerID: 'fake', id: 'fake' })).toBeNull();
    expect(modelOf([opus], null)).toBeNull();
  });
});

describe('providersOf', () => {
  it('lists each provider once, in the order its first model comes', () => {
    const haiku = { ...opus, id: 'claude-haiku-5' };
    expect(providersOf([opus, fake, haiku])).toEqual([opus.provider, fake.provider]);
    expect(providersOf([])).toEqual([]);
  });
});

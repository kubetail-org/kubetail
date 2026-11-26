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

import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { LogsViewSnapshotInput } from '@/gql/graphql';

import type { ModelPick } from './models';

const { sendMock } = vi.hoisted(() => ({ sendMock: vi.fn() }));
vi.mock('urql', () => ({ useMutation: () => [{}, sendMock] }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { ChatOutboxProvider, outboxKey, useChatOutbox } = await import('./chat-outbox');

const accepted = () => ({ data: { chatSend: { id: 'm2', chatID: 'c1', seq: 2, status: 'Streaming' } } });
const refused = (code: string) => ({ error: { graphQLErrors: [{ extensions: { code } }] } });
const dropped = () => ({ error: { networkError: new Error('unreachable'), graphQLErrors: [] } });

const wrapper = ({ children }: { children: ReactNode }) => <ChatOutboxProvider>{children}</ChatOutboxProvider>;

type Mode = 'chat' | 'dashboard';

type Pick = ModelPick;

/** What the composer seeds an entry with: the chat's last answer, or the catalog's first model. */
const seeded: Pick = { model: { providerID: 'fake', id: 'fake' }, effort: 'high' };

// Rerender with another chat, or another cluster, to read what that entry does; the
// provider stays.
const renderOutbox = (
  chatID: string | null = 'c1',
  mode: Mode = 'chat',
  clusterID: string | undefined = '1',
  // Null is "the composer has nothing to seed with": an explicit undefined would
  // take the default instead.
  seed: Pick | null = seeded,
) =>
  renderHook(
    ([m, id, cluster, pick]: [Mode, string | null, string | undefined, Pick | null]) =>
      useChatOutbox(m, id, cluster, pick ?? undefined),
    { wrapper, initialProps: [mode, chatID, clusterID, seed] },
  );

/** The switches as a chat that has not started shows them: sandboxed, with no network. */
const OFF = { sandboxDisabled: false, networkEnabled: false };

/** A view of the chat with nothing on screen. */
const SNAPSHOT: LogsViewSnapshotInput = {
  callID: 'call-1',
  from: null,
  to: null,
  lines: 0,
  atEnd: false,
  pinnedToEnd: false,
  paused: false,
  selected: [],
  selectedCount: 0,
};

const submit = (result: { current: ReturnType<typeof useChatOutbox> }) =>
  act(async () => {
    await result.current.submit(OFF);
  });

beforeEach(() => {
  vi.clearAllMocks();
  sendMock.mockResolvedValue(accepted());
});

describe('outboxKey', () => {
  it('gives the unstarted chat a key per mode, and a chat its own id', () => {
    expect(outboxKey('chat', null)).not.toBe(outboxKey('dashboard', null));
    expect(outboxKey('chat', 'c1')).toBe('c1');
    expect(outboxKey('dashboard', 'c1')).toBe('c1');
  });
});

describe('the draft', () => {
  it('starts empty and idle', () => {
    const { result } = renderOutbox();
    expect(result.current.draft).toBe('');
    expect(result.current.send).toEqual({ status: 'idle' });
  });

  it('belongs to one chat', () => {
    const { result, rerender } = renderOutbox();
    act(() => result.current.setDraft('for c1'));

    rerender(['chat', 'c2', '1', seeded]);
    expect(result.current.draft).toBe('');

    rerender(['chat', 'c1', '1', seeded]);
    expect(result.current.draft).toBe('for c1');
  });

  it('moves to another chat, and is joined onto a draft already waiting there', () => {
    const { result, rerender } = renderOutbox(null);
    act(() => result.current.setDraft('what is a pod'));

    rerender(['chat', 'c1', '1', seeded]);
    act(() => result.current.setDraft('and what is a node'));
    act(() => result.current.moveDraft(null));
    expect(result.current.draft).toBe('');

    rerender(['chat', null, '1', seeded]);
    expect(result.current.draft).toBe('what is a pod\n\nand what is a node');
  });

  it('leaves the destination alone when there is nothing to move', () => {
    const { result, rerender } = renderOutbox(null);
    act(() => result.current.setDraft('what is a pod'));

    rerender(['chat', 'c1', '1', seeded]);
    act(() => result.current.moveDraft(null));

    rerender(['chat', null, '1', seeded]);
    expect(result.current.draft).toBe('what is a pod');
  });
});

// The send settles into the entry rather than through whoever started it, so each
// answer the sidecar can give has to say what it leaves behind.
describe('a send', () => {
  it('sends the draft under a fresh request id, in the wire spelling of the mode', async () => {
    const { result } = renderOutbox(null, 'dashboard');
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(sendMock).toHaveBeenCalledWith({
      chatID: null,
      mode: 'Dashboard',
      clusterID: '1',
      sandboxDisabled: false,
      networkEnabled: false,
      networkThisTurn: false,
      providerID: seeded.model.providerID,
      modelID: seeded.model.id,
      effort: seeded.effort,
      requestID: expect.any(String),
      content: 'hello',
      snapshot: null,
    });
  });

  // The sidecar refuses a send whose switch differs from the chat's, so the send
  // says which one the user saw.
  it('sends the switch it is handed', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await act(async () => {
      await result.current.submit({ ...OFF, sandboxDisabled: true });
    });
    expect(sendMock.mock.calls[0][0]).toMatchObject({ sandboxDisabled: true });
  });

  // The sidecar refuses a send whose network switch differs from the chat's too,
  // and the toggle is the entry's: it rides the send and goes once one is accepted.
  it('sends the network switch it is handed and the toggle, and clears the toggle once accepted', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    act(() => result.current.setNetworkThisTurn(true));
    expect(result.current.networkThisTurn).toBe(true);
    await act(async () => {
      await result.current.submit({ ...OFF, networkEnabled: true });
    });

    expect(sendMock.mock.calls[0][0]).toMatchObject({ networkEnabled: true, networkThisTurn: true });
    expect(result.current.networkThisTurn).toBe(false);
  });

  it('keeps the toggle when a send is refused', async () => {
    sendMock.mockResolvedValue(refused('KUBETAIL_CHAT_NETWORK_CHANGED'));
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    act(() => result.current.setNetworkThisTurn(true));
    await submit(result);

    expect(result.current.networkThisTurn).toBe(true);
    expect(result.current.refusal).toEqual({ kind: 'network-changed' });
    expect(result.current.draft).toBe('hello');
  });

  it('holds the draft read-only while in flight', async () => {
    let resolve!: (value: unknown) => void;
    sendMock.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));

    let sending!: Promise<unknown>;
    act(() => {
      sending = result.current.submit(OFF);
    });
    expect(result.current.send).toMatchObject({ status: 'sending', content: 'hello' });
    expect(result.current.draft).toBe('hello');

    await act(async () => {
      resolve(accepted());
      await sending;
    });
  });

  it('clears the draft once accepted and waits for the row', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(result.current.draft).toBe('');
    expect(result.current.send).toEqual({ status: 'awaiting', seq: 2 });
  });

  // The route the created chat opens delivers the row with its snapshot, and the
  // unstarted entry watches no messages that could ever settle an `awaiting`.
  it('resolves to the created chat and leaves the unstarted entry idle', async () => {
    const { result } = renderOutbox(null);
    act(() => result.current.setDraft('hello'));

    let created;
    await act(async () => {
      created = await result.current.submit(OFF);
    });

    // The cluster rides back with the id, for a caller deciding whether to follow the
    // chat: a send can land after the window has moved to another cluster.
    expect(created).toEqual({ chatID: 'c1', clusterID: '1' });
    expect(result.current.draft).toBe('');
    expect(result.current.send).toEqual({ status: 'idle' });
  });

  it('resolves to nothing when the chat already existed', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));

    let created;
    await act(async () => {
      created = await result.current.submit(OFF);
    });

    expect(created).toBeNull();
  });

  it('keeps the draft and settles when refused', async () => {
    sendMock.mockResolvedValue(refused('KUBETAIL_CONFLICT'));
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(result.current.draft).toBe('hello');
    expect(result.current.send).toEqual({ status: 'idle' });
  });

  // A full chat is the one refusal the composer draws, so it is kept on the
  // entry; every other refusal leaves nothing behind.
  it('keeps a refusal for a full chat on the entry', async () => {
    sendMock.mockResolvedValue(refused('KUBETAIL_CHAT_CONTEXT_FULL'));
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(result.current.draft).toBe('hello');
    expect(result.current.send).toEqual({ status: 'idle' });
    expect(result.current.refusal).toEqual({ kind: 'context-full', model: seeded.model });

    sendMock.mockResolvedValue(refused('KUBETAIL_CONFLICT'));
    await submit(result);
    expect(result.current.refusal).toBeNull();
  });

  it('keeps a refusal for a changed switch on the entry', async () => {
    sendMock.mockResolvedValue(refused('KUBETAIL_CHAT_SANDBOX_CHANGED'));
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(result.current.draft).toBe('hello');
    expect(result.current.send).toEqual({ status: 'idle' });
    expect(result.current.refusal).toEqual({ kind: 'sandbox-changed' });
  });

  it('clears the refusal on the next send', async () => {
    sendMock.mockResolvedValue(refused('KUBETAIL_CHAT_CONTEXT_FULL'));
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);
    expect(result.current.refusal).toEqual({ kind: 'context-full', model: seeded.model });

    sendMock.mockResolvedValue(accepted());
    await submit(result);
    expect(result.current.refusal).toBeNull();
  });

  it('holds a send that got no answer', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(result.current.draft).toBe('hello');
    expect(result.current.send).toMatchObject({ status: 'held', content: 'hello' });
  });

  it('sends the snapshot it is handed, and retries a held send with it', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft("what's this?"));
    await act(async () => {
      await result.current.submit(OFF, SNAPSHOT);
    });
    expect(sendMock.mock.calls[0][0].snapshot).toBe(SNAPSHOT);

    sendMock.mockResolvedValue(accepted());
    await act(async () => {
      await result.current.retry();
    });
    expect(sendMock.mock.calls[1][0].snapshot).toBe(SNAPSHOT);
  });

  // Committed-but-lost is answered by the request id alone, so the retry goes back
  // under the id the first attempt used.
  it('retries a held send unchanged', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    sendMock.mockResolvedValue(accepted());
    await act(async () => {
      await result.current.retry();
    });

    expect(sendMock.mock.calls[1][0]).toEqual(sendMock.mock.calls[0][0]);
    expect(result.current.send).toEqual({ status: 'awaiting', seq: 2 });
  });

  it('mints a fresh id for the submit after a refused retry', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    sendMock.mockResolvedValue(refused('KUBETAIL_CONFLICT'));
    await act(async () => {
      await result.current.retry();
    });
    expect(result.current.send).toEqual({ status: 'idle' });

    sendMock.mockResolvedValue(accepted());
    await submit(result);
    expect(sendMock.mock.calls[2][0].requestID).not.toBe(sendMock.mock.calls[0][0].requestID);
  });

  it('retries nothing when nothing is held', async () => {
    const { result } = renderOutbox();

    let created;
    await act(async () => {
      created = await result.current.retry();
    });

    expect(created).toBeNull();
    expect(sendMock).not.toHaveBeenCalled();
  });

  it('returns a discarded send to an ordinary draft', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    act(() => result.current.discard());
    expect(result.current.send).toEqual({ status: 'idle' });
    expect(result.current.draft).toBe('hello');
  });

  // The whole reason the entries live above the routes.
  it('settles into its own entry after the box has moved to another chat', async () => {
    let resolve!: (value: unknown) => void;
    sendMock.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const { result, rerender } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    act(() => {
      void result.current.submit(OFF);
    });

    rerender(['chat', 'c2', '1', seeded]);
    await act(async () => {
      resolve(accepted());
    });

    rerender(['chat', 'c1', '1', seeded]);
    expect(result.current.draft).toBe('');
    expect(result.current.send).toEqual({ status: 'awaiting', seq: 2 });
  });

  // The composer is disabled without a cluster, so this is the guard behind it
  // rather than a path a user reaches.
  it('sends nothing for a new chat with no cluster to file it under', async () => {
    // Its own render: `renderOutbox` defaults the cluster, and absent is the case.
    const { result } = renderHook(() => useChatOutbox('chat', null), { wrapper });
    act(() => result.current.setDraft('hello'));

    let created;
    await act(async () => {
      created = await result.current.submit(OFF);
    });

    expect(created).toBeNull();
    expect(sendMock).not.toHaveBeenCalled();
    expect(result.current.send).toEqual({ status: 'idle' });
    expect(result.current.draft).toBe('hello');
  });

  // A retry is the same send, so it goes back under the cluster it left with — not
  // whatever the window has switched to since.
  it('retries a held send under the cluster it left with', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result, rerender } = renderOutbox(null, 'chat', '1');
    act(() => result.current.setDraft('hello'));
    await submit(result);

    rerender(['chat', null, '2', seeded]);
    sendMock.mockResolvedValue(accepted());
    let created;
    await act(async () => {
      created = await result.current.retry();
    });

    expect(sendMock.mock.calls[1][0]).toEqual(sendMock.mock.calls[0][0]);
    expect(sendMock.mock.calls[1][0].clusterID).toBe('1');
    expect(created).toEqual({ chatID: 'c1', clusterID: '1' });
  });

  // The pick is the entry's, so it survives the composer unmounting — and a chat
  // that has not been picked for takes what the composer seeds it with.
  it('sends the seed until the entry is picked for', async () => {
    const { result } = renderOutbox();
    expect(result.current.pick).toEqual(seeded);

    act(() => result.current.setPick({ model: { providerID: 'anthropic', id: 'claude-opus-5' }, effort: 'low' }));
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(sendMock.mock.calls[0][0]).toMatchObject({
      providerID: 'anthropic',
      modelID: 'claude-opus-5',
      effort: 'low',
    });
  });

  it('belongs to one chat, like the draft', () => {
    const { result, rerender } = renderOutbox();
    act(() => result.current.setPick({ model: { providerID: 'anthropic', id: 'claude-opus-5' }, effort: 'low' }));

    rerender(['chat', 'c2', '1', seeded]);
    expect(result.current.pick).toEqual(seeded);
  });

  // Nothing to seed with means the catalog has not answered, and a send with no
  // model is one the sidecar would refuse.
  it('sends nothing with no pick', async () => {
    const { result } = renderOutbox('c1', 'chat', '1', null);
    act(() => result.current.setDraft('hello'));
    await submit(result);

    expect(sendMock).not.toHaveBeenCalled();
    expect(result.current.draft).toBe('hello');
  });

  // A retry is the same send, so it runs what the user sent — not what the select
  // has moved to since.
  it('retries a held send with the pick it was held with', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    act(() => result.current.setPick({ model: { providerID: 'anthropic', id: 'claude-opus-5' }, effort: 'low' }));
    sendMock.mockResolvedValue(accepted());
    await act(async () => {
      await result.current.retry();
    });

    expect(sendMock.mock.calls[1][0]).toEqual(sendMock.mock.calls[0][0]);
  });

  // A retry is the same send, so it says what the user saw when they sent it.
  it('retries a held send with the switch it was held with', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await act(async () => {
      await result.current.submit({ ...OFF, sandboxDisabled: true });
    });

    sendMock.mockResolvedValue(accepted());
    await act(async () => {
      await result.current.retry();
    });
    expect(sendMock.mock.calls[1][0]).toMatchObject({ sandboxDisabled: true });
  });

  // A held send keeps the switches and the toggle it left with, and its retry
  // sends them as held, whatever the entry's toggle says by then.
  it('retries a held send with the network it was held with', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    act(() => result.current.setNetworkThisTurn(true));
    await act(async () => {
      await result.current.submit({ ...OFF, networkEnabled: true });
    });
    expect(result.current.networkThisTurn).toBe(true);
    act(() => result.current.setNetworkThisTurn(false));

    sendMock.mockResolvedValue(accepted());
    await act(async () => {
      await result.current.retry();
    });
    expect(sendMock.mock.calls[1][0]).toMatchObject({ networkEnabled: true, networkThisTurn: true });
  });

  // An accepted send stays closed until its row reaches the watch; whoever watches
  // the messages settles it, and the entry is idle again for the next thing the
  // window wants to send — Ask again included.
  it('settles once the row it is awaiting arrives', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);
    expect(result.current.send).toEqual({ status: 'awaiting', seq: 2 });

    // An earlier row is not the row this send is waiting for.
    act(() => result.current.settle(1));
    expect(result.current.send).toEqual({ status: 'awaiting', seq: 2 });

    act(() => result.current.settle(2));
    expect(result.current.send).toEqual({ status: 'idle' });

    await act(async () => {
      await result.current.askAgain('what is a pod?', seeded, OFF);
    });
    expect(sendMock).toHaveBeenCalledTimes(2);
  });

  it('settles nothing that is not awaiting', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();
    act(() => result.current.setDraft('hello'));
    await submit(result);

    act(() => result.current.settle(99));
    expect(result.current.send).toMatchObject({ status: 'held' });
  });

  // Ask again is the user asking twice — a fresh send, not the held send's retry.
  it('asks the failed question again under a new id, with the pick it names', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setDraft('a follow-up I have not sent'));

    await act(async () => {
      await result.current.askAgain('what is a pod?', seeded, OFF);
    });

    expect(sendMock).toHaveBeenCalledWith({
      chatID: 'c1',
      mode: 'Chat',
      clusterID: '1',
      sandboxDisabled: false,
      networkEnabled: false,
      networkThisTurn: false,
      providerID: seeded.model.providerID,
      modelID: seeded.model.id,
      effort: seeded.effort,
      requestID: expect.any(String),
      content: 'what is a pod?',
      snapshot: null,
    });
    // What submit sends is the draft; what this sends came off the failed row, so
    // clearing would take a follow-up the user typed and never sent.
    expect(result.current.draft).toBe('a follow-up I have not sent');
  });

  it("rides the entry's one send, so a second is refused while the first is not settled", async () => {
    let resolve!: (value: unknown) => void;
    sendMock.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const { result } = renderOutbox();
    const pick: Pick = seeded;

    act(() => {
      void result.current.askAgain('what is a pod?', pick, OFF);
    });
    expect(result.current.send.status).toBe('sending');

    await act(async () => {
      await result.current.askAgain('what is a pod?', pick, OFF);
    });
    expect(sendMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolve(accepted());
    });
  });

  // The toggle is the draft's: Ask again neither sends it nor clears it, so a
  // follow-up the user set it for still goes out with it.
  it('asks again without the toggle and leaves it on', async () => {
    const { result } = renderOutbox();
    act(() => result.current.setNetworkThisTurn(true));

    await act(async () => {
      await result.current.askAgain('what is a pod?', seeded, OFF);
    });

    expect(sendMock.mock.calls[0][0]).toMatchObject({ networkThisTurn: false });
    expect(result.current.networkThisTurn).toBe(true);
  });

  // A held Ask again is still not the draft's send: what it carries came off the
  // failed row, and a follow-up typed since must survive the retry.
  it('retries a held ask again without taking the draft', async () => {
    sendMock.mockResolvedValue(dropped());
    const { result } = renderOutbox();

    await act(async () => {
      await result.current.askAgain('what is a pod?', seeded, OFF);
    });
    act(() => result.current.setDraft('a follow-up I have not sent'));
    expect(result.current.send).toMatchObject({ status: 'held', content: 'what is a pod?' });

    sendMock.mockResolvedValue(accepted());
    await act(async () => {
      await result.current.retry();
    });

    expect(result.current.draft).toBe('a follow-up I have not sent');
  });

  it('refuses to work outside its provider', () => {
    expect(() => renderHook(() => useChatOutbox('chat', 'c1', '1'))).toThrow(/ChatOutboxProvider/);
  });
});

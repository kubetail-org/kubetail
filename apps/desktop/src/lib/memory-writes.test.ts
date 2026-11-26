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

import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { saveMock, deleteMock } = vi.hoisted(() => ({ saveMock: vi.fn(), deleteMock: vi.fn() }));
vi.mock('urql', () => ({
  useMutation: (doc: unknown) => [{}, JSON.stringify(doc).includes('memoryDelete') ? deleteMock : saveMock],
}));

const { bodyFits, nameFor, slugify, uniqueName, useMemoryWrites } = await import('./memory-writes');

const refused = (code: string) => ({ error: { graphQLErrors: [{ extensions: { code } }] } });
const writes = () => renderHook(() => useMemoryWrites()).result.current;
const input = { id: 'm1', clusterID: 'c1', name: 'pages', body: 'Treat its pods as prod.' };

beforeEach(() => {
  vi.clearAllMocks();
  saveMock.mockResolvedValue({ data: { memorySave: { id: 'm1' } } });
  deleteMock.mockResolvedValue({ data: { memoryDelete: true } });
});

describe('useMemoryWrites', () => {
  it('sends a save as given and answers null', async () => {
    expect(await writes().save(input)).toBeNull();
    expect(saveMock).toHaveBeenCalledWith({ input });
  });

  it.each([
    ['KUBETAIL_RECORD_NOT_FOUND', 'This note or its cluster is gone.'],
    ['KUBETAIL_MEMORY_NAME_TAKEN', 'Another note here starts the same way. Reword its opening.'],
    ['KUBETAIL_MEMORY_FULL', 'There is no room for this note here. Shorten or delete one first.'],
    ['KUBETAIL_MEMORY_SECRET', 'This looks like a credential. Notes never keep one.'],
    ['KUBETAIL_VALIDATION_ERROR', 'A note is at most 500 bytes.'],
    ['KUBETAIL_SOMETHING_ELSE', 'The note could not be saved.'],
  ])('says why a save was refused with %s', async (code, message) => {
    saveMock.mockResolvedValue(refused(code));
    expect(await writes().save(input)).toBe(message);
  });

  it('deletes by id and answers whether the memory is gone', async () => {
    expect(await writes().remove('m1')).toBe(true);
    expect(deleteMock).toHaveBeenCalledWith({ id: 'm1' });

    deleteMock.mockResolvedValueOnce({ error: { graphQLErrors: [], message: 'socket closed' } });
    expect(await writes().remove('m1')).toBe(false);
  });

  it('counts a memory already gone as deleted', async () => {
    deleteMock.mockResolvedValue(refused('KUBETAIL_RECORD_NOT_FOUND'));
    expect(await writes().remove('m1')).toBe(true);
  });
});

describe('slugify', () => {
  it('joins lower-case words with single hyphens', () => {
    expect(slugify('Page the Payments team')).toBe('page-the-payments-team');
    expect(slugify('  prod, then   staging!! ')).toBe('prod-then-staging');
    expect(slugify('--v2 rollout--')).toBe('v2-rollout');
  });

  it('folds accented Latin to ASCII and drops other scripts', () => {
    expect(slugify('Café prod')).toBe('cafe-prod');
    expect(slugify('naïve')).toBe('naive');
    expect(slugify('本番環境')).toBe('');
  });

  it('cuts at 48 on a word boundary', () => {
    const words = `${'a'.repeat(40)} ${'b'.repeat(7)}`; // 48 characters
    expect(slugify(`${words} cc`)).toBe(`${'a'.repeat(40)}-${'b'.repeat(7)}`);
    expect(slugify(`${words}c dd`)).toBe('a'.repeat(40));
    expect(slugify('a'.repeat(60))).toBe('a'.repeat(48));
  });
});

describe('uniqueName', () => {
  it('suffixes a taken name until one is free', () => {
    expect(uniqueName('oncall', new Set())).toBe('oncall');
    expect(uniqueName('oncall', new Set(['oncall']))).toBe('oncall-2');
    expect(uniqueName('oncall', new Set(['oncall', 'oncall-2']))).toBe('oncall-3');
    const nine = ['oncall', ...Array.from({ length: 8 }, (_, i) => `oncall-${i + 2}`)];
    expect(uniqueName('oncall', new Set(nine))).toBe('oncall-10');
  });

  it('cuts a long name to fit its suffix, leaving no double hyphen', () => {
    const base = `${'a'.repeat(45)}-bc`;
    expect(uniqueName(base, new Set([base]))).toBe(`${'a'.repeat(45)}-2`);
  });
});

describe('nameFor', () => {
  it("slugs the body's opening words, clear of the names taken", () => {
    expect(nameFor('Page payments.', new Set())).toBe('page-payments');
    expect(nameFor('Page payments.', new Set(['page-payments']))).toBe('page-payments-2');
  });

  it('falls back to "note" for a body with no Latin letters', () => {
    expect(nameFor('本番環境', new Set())).toBe('note');
    expect(nameFor('本番環境', new Set(['note']))).toBe('note-2');
  });
});

describe('bodyFits', () => {
  it('measures a body in bytes', () => {
    expect(bodyFits('é'.repeat(250))).toBe(true);
    expect(bodyFits('é'.repeat(251))).toBe(false);
  });
});

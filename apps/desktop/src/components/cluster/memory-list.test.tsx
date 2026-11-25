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
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';

import type { Memory } from '@/lib/memories';
import { expectProseText } from '@/test-utils';

import { MemoryList } from './memory-list';

vi.mock('@/lib/dialog', () => ({ useDialogHost: () => null }));

const BODY = 'This cluster routes through traefik, not nginx.';

const memory = (i: number, over: Partial<Memory> = {}): Memory => ({
  id: `m${i}`,
  clusterID: 'c1',
  name: `note-${i}`,
  body: BODY,
  writtenBy: 'Model',
  updatedAt: new Date(Date.now() - 3 * 86_400_000).toISOString(),
  ...over,
});
const THREE = Array.from({ length: 3 }, (_, i) => memory(i));

type Saved = { memory: Memory | undefined; name: string; body: string };

// The notes are the parent's; this gives them somewhere to live. `refusal`
// stands in for the sidecar's answer to a save, `failing` for a delete that
// fails once.
function List({
  initial = THREE,
  adding: startAdding = false,
  refusal = null,
  failing = false,
  saves = [],
  pending,
}: {
  initial?: Memory[];
  adding?: boolean;
  refusal?: string | null;
  failing?: boolean;
  saves?: Saved[];
  // Holds every save until it resolves.
  pending?: Promise<void>;
}) {
  const [memories, setMemories] = useState(initial);
  const [stuck, setStuck] = useState(failing);
  const [adding, setAdding] = useState(startAdding);
  return (
    <>
      <MemoryList
        scope="Minikube"
        memories={memories}
        adding={adding}
        onSave={async (m, name, body) => {
          saves.push({ memory: m, name, body });
          await pending;
          if (refusal) return refusal;
          setMemories((prev) =>
            m
              ? prev.map((x) => (x.id === m.id ? { ...x, body, writtenBy: 'User' } : x))
              : [memory(prev.length + 10, { name, body, writtenBy: 'User' }), ...prev],
          );
          return null;
        }}
        onAddDone={() => setAdding(false)}
        onDelete={async (id) => {
          if (stuck) {
            setStuck(false);
            return false;
          }
          setMemories((prev) => prev.filter((m) => m.id !== id));
          return true;
        }}
      />
      <button type="button" onClick={() => setAdding(true)}>
        Add
      </button>
      <button type="button">Elsewhere</button>
    </>
  );
}

const button = (name: string) => screen.getByRole('button', { name });
const editNote = (user: ReturnType<typeof userEvent.setup>, i = 0) =>
  user.click(screen.getAllByRole('button', { name: /^Edit note: / })[i]);
// The age is its own element, so the line is matched whole.
const provenance = (text: string) => screen.getByText((_, el) => el?.tagName === 'P' && el.textContent === text);
const editor = () => screen.getByRole<HTMLTextAreaElement>('textbox', { name: 'Note' });

describe('MemoryList', () => {
  it('says so when there are no notes', () => {
    render(<List initial={[]} />);
    expect(screen.getByText('No notes yet.')).toBeInTheDocument();
  });

  it('draws who wrote each note and how long ago, never its name', () => {
    render(<List initial={[memory(0), memory(1, { writtenBy: 'User' })]} />);
    expect(provenance('By Kubetail · 3d ago')).toBeInTheDocument();
    expect(provenance('By you · 3d ago')).toBeInTheDocument();
    expect(screen.queryByText(/note-0/)).not.toBeInTheDocument();
  });

  it("names each note's edit button by its body", () => {
    render(<List initial={[memory(0), memory(1, { body: 'Page payments.' })]} />);
    expect(screen.getByRole('button', { name: `Edit note: ${BODY}` })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Edit note: Page payments.' })).toBeInTheDocument();
  });

  it('draws a body as its characters, never markup', () => {
    render(<List initial={[memory(0, { body: '# Heading\n<b>bold</b>' })]} />);
    const body = screen.getByText(/# Heading/);
    expect(body).toHaveTextContent('<b>bold</b>');
    expect(body.querySelector('b')).toBeNull();
    expect(document.querySelector('h1')).toBeNull();
  });

  it('divides the notes, with no line above the first', () => {
    render(<List />);
    expect(screen.getAllByRole('separator', { hidden: true })).toHaveLength(2);
  });

  // jsdom applies no CSS, so this checks the classes that decide what a browser
  // hides: the trash button only fades, never leaving the tab order.
  it("keeps a note's trash button in reach of the keyboard", () => {
    render(<List initial={[memory(0)]} />);
    const trash = button(`Delete note: ${BODY}`);
    expect(trash).toHaveClass('opacity-0', 'group-focus-within/row:opacity-100', 'group-hover/row:opacity-100');
    expect(trash).not.toHaveClass('hidden', 'invisible');
  });

  it("names each trash button by its note's body", () => {
    render(<List initial={[memory(0, { body: 'First.' }), memory(1, { body: 'Second.' })]} />);
    expect(button('Delete note: First.')).toBeInTheDocument();
    expect(button('Delete note: Second.')).toBeInTheDocument();
  });

  describe('editing', () => {
    it('opens on a click of the text, selected, with spellcheck alone', async () => {
      const user = userEvent.setup();
      render(<List />);
      await editNote(user);
      expect(editor()).toHaveValue(BODY);
      expect(editor()).toHaveFocus();
      expect([editor().selectionStart, editor().selectionEnd]).toEqual([0, BODY.length]);
      expectProseText(editor());
      // The note being edited draws no trash button.
      expect(screen.getAllByRole('button', { name: `Delete note: ${BODY}` })).toHaveLength(2);
    });

    it('breaks a line on Enter and saves on Cmd+Enter', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.clear(editor());
      await user.type(editor(), '  Uses nginx.{Enter}Really.  ');
      expect(editor()).toHaveValue('  Uses nginx.\nReally.  ');

      await user.keyboard('{Meta>}{Enter}{/Meta}');
      expect(saves).toEqual([{ memory: THREE[0], name: 'note-0', body: 'Uses nginx.\nReally.' }]);
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
      expect(screen.getByText(/^Uses nginx\./)).toBeInTheDocument();
      expect(provenance('By you · 3d ago')).toBeInTheDocument();
    });

    it('saves on Ctrl+Enter', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.type(editor(), ' Twice.');
      await user.keyboard('{Control>}{Enter}{/Control}');
      expect(saves.map((s) => s.body)).toEqual([`${BODY} Twice.`]);
    });

    it('saves when the field loses focus', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.type(editor(), ' Twice.');
      await user.click(button('Elsewhere'));
      expect(saves.map((s) => s.body)).toEqual([`${BODY} Twice.`]);
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    });

    it('discards the draft on Escape, saving nothing', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.type(editor(), ' Never mind.');
      await user.keyboard('{Escape}');
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
      expect(screen.getAllByText(BODY)).toHaveLength(3);
      expect(saves).toEqual([]);
    });

    it('leaves Escape and Cmd+Enter to an IME composing text', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.type(editor(), ' 日本');
      fireEvent.keyDown(editor(), { key: 'Escape', isComposing: true });
      fireEvent.keyDown(editor(), { key: 'Enter', metaKey: true, isComposing: true });
      expect(editor()).toHaveValue(`${BODY} 日本`);
      expect(saves).toEqual([]);
    });

    it('just closes on a blank or unchanged draft', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} initial={THREE.map((m) => ({ ...m, writtenBy: 'User' as const }))} />);
      await editNote(user);
      await user.keyboard('{Meta>}{Enter}{/Meta}');
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();

      await editNote(user);
      await user.clear(editor());
      await user.type(editor(), '   ');
      await user.click(button('Elsewhere'));
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
      expect(screen.getAllByText(BODY)).toHaveLength(3);
      expect(saves).toEqual([]);
    });

    it("adopts one of Kubetail's notes on an explicit save, even unchanged", async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.keyboard('{Meta>}{Enter}{/Meta}');
      expect(saves).toEqual([{ memory: THREE[0], name: 'note-0', body: BODY }]);
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
      expect(provenance('By you · 3d ago')).toBeInTheDocument();
    });

    it("never adopts one of Kubetail's notes on a blur alone", async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.click(button('Elsewhere'));
      expect(saves).toEqual([]);
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    });

    it('saves nothing on a blur when only the watch changed the note', async () => {
      const user = userEvent.setup();
      const onSave = vi.fn(async () => null);
      const props = { scope: 'Minikube', adding: false, onSave, onAddDone: () => {}, onDelete: async () => true };
      const { rerender } = render(
        <>
          <MemoryList {...props} memories={[memory(0)]} />
          <button type="button">Elsewhere</button>
        </>,
      );
      await editNote(user);
      rerender(
        <>
          <MemoryList {...props} memories={[memory(0, { body: 'Rewritten by Kubetail.' })]} />
          <button type="button">Elsewhere</button>
        </>,
      );
      expect(editor()).toHaveValue(BODY);

      await user.click(button('Elsewhere'));
      expect(onSave).not.toHaveBeenCalled();
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    });

    it("says saving one of Kubetail's notes makes it yours, and nothing for yours", async () => {
      const user = userEvent.setup();
      const line = 'Saving makes this your note: Kubetail will follow it.';
      render(<List initial={[memory(0), memory(1, { writtenBy: 'User' })]} />);
      await editNote(user, 0);
      expect(screen.getByText(line)).toBeInTheDocument();
      await user.keyboard('{Escape}');

      await editNote(user, 1);
      expect(screen.queryByText(line)).not.toBeInTheDocument();
    });

    it('keeps the draft on a refused save and says why', async () => {
      const user = userEvent.setup();
      render(<List refusal="This looks like a credential. Notes never keep one." />);
      await editNote(user);
      await user.type(editor(), ' token=abc');
      await user.keyboard('{Meta>}{Enter}{/Meta}');
      expect(await screen.findByText('This looks like a credential. Notes never keep one.')).toHaveClass(
        'text-destructive',
      );
      expect(editor()).toHaveValue(`${BODY} token=abc`);
    });

    it('draws the rule a body over 500 bytes breaks, and holds the save', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List saves={saves} />);
      await editNote(user);
      await user.clear(editor());
      await user.click(editor());
      await user.paste('é'.repeat(251));
      expect(screen.getByText('At most 500 bytes.')).toHaveClass('text-destructive');

      await user.keyboard('{Meta>}{Enter}{/Meta}');
      await user.click(button('Elsewhere'));
      expect(saves).toEqual([]);
      expect(editor()).toBeInTheDocument();
    });

    describe('while a save is in flight', () => {
      // Holds every save until `release` is called.
      const held = () => {
        let release = () => {};
        const pending = new Promise<void>((resolve) => {
          release = resolve;
        });
        return { pending, release: () => release() };
      };

      it('ignores a second save', async () => {
        const user = userEvent.setup();
        const saves: Saved[] = [];
        const { pending, release } = held();
        render(<List saves={saves} pending={pending} />);
        await editNote(user);
        await user.type(editor(), ' Twice.');
        await user.keyboard('{Meta>}{Enter}{/Meta}');
        await user.keyboard('{Meta>}{Enter}{/Meta}');
        expect(saves).toHaveLength(1);

        release();
        await screen.findByText(`${BODY} Twice.`);
      });

      it('takes no typing, so nothing typed after the save is dropped', async () => {
        const user = userEvent.setup();
        const { pending, release } = held();
        render(<List pending={pending} />);
        await editNote(user);
        await user.type(editor(), ' Twice.');
        await user.keyboard('{Meta>}{Enter}{/Meta}');
        expect(editor()).toHaveAttribute('readonly');
        await user.type(editor(), ' Thrice.');
        expect(editor()).toHaveValue(`${BODY} Twice.`);

        release();
        await screen.findByText(`${BODY} Twice.`);
      });

      it('waits out Escape, since the save is already sent', async () => {
        const user = userEvent.setup();
        const saves: Saved[] = [];
        const { pending, release } = held();
        render(<List saves={saves} pending={pending} />);
        await editNote(user);
        await user.type(editor(), ' Twice.');
        await user.keyboard('{Meta>}{Enter}{/Meta}');
        await user.keyboard('{Escape}');
        expect(editor()).toBeInTheDocument();

        release();
        await screen.findByText(`${BODY} Twice.`);
        expect(saves).toHaveLength(1);
      });
    });

    describe('starting another note', () => {
      const editors = () => screen.getAllByRole('textbox', { name: 'Note' });

      it('saves the first on its way out', async () => {
        const user = userEvent.setup();
        const saves: Saved[] = [];
        render(<List saves={saves} />);
        await editNote(user, 0);
        await user.type(editor(), ' Twice.');
        // Note 0 is the editor now, so the first text left is note 1's.
        await editNote(user, 0);
        expect(saves.map((s) => s.memory?.id)).toEqual(['m0']);
        expect(await screen.findByText(`${BODY} Twice.`)).toBeInTheDocument();
        expect(editor()).toHaveValue(BODY);
      });

      it('keeps the first open with its draft and reason when its save is refused', async () => {
        const user = userEvent.setup();
        render(<List refusal="This looks like a credential. Notes never keep one." />);
        await editNote(user, 0);
        await user.type(editor(), ' token=abc');
        await editNote(user, 0);

        expect(await screen.findByText('This looks like a credential. Notes never keep one.')).toBeInTheDocument();
        expect(editors().map((e) => (e as HTMLTextAreaElement).value)).toEqual([`${BODY} token=abc`, BODY]);
      });

      it('keeps the first open when its draft is over 500 bytes', async () => {
        const user = userEvent.setup();
        const saves: Saved[] = [];
        render(<List saves={saves} />);
        await editNote(user, 0);
        await user.clear(editor());
        await user.paste('é'.repeat(251));
        await editNote(user, 0);

        expect(screen.getByText('At most 500 bytes.')).toBeInTheDocument();
        expect(editors()).toHaveLength(2);
        expect(editors()[0]).toHaveValue('é'.repeat(251));
        expect(saves).toEqual([]);
      });
    });
  });

  describe('adding', () => {
    const newNote = () => screen.getByRole('textbox', { name: 'New note' });

    it('opens empty and focused at the top of the list', () => {
      render(<List adding />);
      expect(newNote()).toHaveValue('');
      expect(newNote()).toHaveFocus();
      expect(newNote()).toHaveAttribute('placeholder', 'What should Kubetail remember?');
      expect(newNote().compareDocumentPosition(screen.getAllByText(BODY)[0])).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
      expect(screen.getAllByRole('separator', { hidden: true })).toHaveLength(3);
      expect(screen.queryByText(/Saving makes this your note/)).not.toBeInTheDocument();
    });

    it('opens into an empty list too', () => {
      render(<List initial={[]} adding />);
      expect(newNote()).toBeInTheDocument();
      expect(screen.queryByText('No notes yet.')).not.toBeInTheDocument();
    });

    it("names a note after its body's opening words, clear of the cluster's names", async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List initial={[memory(0, { name: 'page-payments' })]} adding saves={saves} />);
      await user.type(newNote(), 'Page payments.');
      await user.keyboard('{Meta>}{Enter}{/Meta}');
      expect(saves).toEqual([{ memory: undefined, name: 'page-payments-2', body: 'Page payments.' }]);
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
      expect(screen.getByText('Page payments.')).toBeInTheDocument();
    });

    it('names a body with no Latin letters "note"', async () => {
      const user = userEvent.setup();
      const saves: Saved[] = [];
      render(<List adding saves={saves} />);
      await user.type(newNote(), '本番環境');
      await user.click(button('Elsewhere'));
      expect(saves.map((s) => s.name)).toEqual(['note']);
    });

    it('closes on Escape or when left empty', async () => {
      const user = userEvent.setup();
      render(<List adding />);
      await user.type(newNote(), 'Never mind.');
      await user.keyboard('{Escape}');
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();

      await user.click(button('Add'));
      await user.click(button('Elsewhere'));
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    });

    it('stays open on a refused save', async () => {
      const user = userEvent.setup();
      render(<List adding refusal="There is no room for this note here. Shorten or delete one first." />);
      await user.type(newNote(), 'Page payments.');
      await user.keyboard('{Meta>}{Enter}{/Meta}');
      expect(await screen.findByText(/^There is no room/)).toBeInTheDocument();
      expect(newNote()).toHaveValue('Page payments.');
    });
  });

  describe('deleting', () => {
    it('deletes a note after confirming', async () => {
      const user = userEvent.setup();
      render(<List />);
      await user.click(screen.getAllByRole('button', { name: `Delete note: ${BODY}` })[0]);
      expect(await screen.findByText('Delete note?')).toBeInTheDocument();
      expect(screen.getByText(/permanently deleted from Minikube/)).toBeInTheDocument();

      await user.click(screen.getByRole('button', { name: 'Delete note' }));
      expect(screen.getAllByText(BODY)).toHaveLength(2);
      expect(screen.queryByText('Delete note?')).not.toBeInTheDocument();
    });

    it('keeps the note when the confirm is cancelled', async () => {
      const user = userEvent.setup();
      render(<List />);
      await user.click(screen.getAllByRole('button', { name: `Delete note: ${BODY}` })[0]);
      await user.click(await screen.findByRole('button', { name: 'Cancel' }));
      expect(screen.getAllByText(BODY)).toHaveLength(3);
    });

    it('keeps the confirm and says so when a delete fails, and a retry deletes it', async () => {
      const user = userEvent.setup();
      render(<List failing />);
      await user.click(screen.getAllByRole('button', { name: `Delete note: ${BODY}` })[0]);
      const confirm = async () => user.click(await screen.findByRole('button', { name: 'Delete note' }));

      await confirm();
      expect(await screen.findByText('The note could not be deleted.')).toBeInTheDocument();
      expect(screen.getByText('Delete note?')).toBeInTheDocument();

      await confirm();
      expect(screen.getAllByText(BODY)).toHaveLength(2);
      expect(screen.queryByText('The note could not be deleted.')).not.toBeInTheDocument();
    });

    it('deletes down to the empty state', async () => {
      const user = userEvent.setup();
      render(<List initial={[memory(0)]} />);
      await user.click(button(`Delete note: ${BODY}`));
      await user.click(await screen.findByRole('button', { name: 'Delete note' }));
      expect(screen.getByText('No notes yet.')).toBeInTheDocument();
    });
  });
});

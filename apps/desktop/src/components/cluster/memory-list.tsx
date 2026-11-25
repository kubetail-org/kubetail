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

// One scope's notes, in one bordered card. A note's text edits in place on a
// click, and its trash button deletes it after a confirm. A note may have been
// written by the model from cluster data, so its body is drawn as text, never
// as markdown. The name is the model's key and is never shown.
import { Button } from '@kubetail/ui/elements/button';
import { Separator } from '@kubetail/ui/elements/separator';
import { Trash2 } from 'lucide-react';
import { Fragment, useEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';

import { Dialog } from '@/components/primitives/dialog';
import { RelativeTime } from '@/components/primitives/relative-time';
import { Textarea } from '@/components/primitives/text-entry';
import type { Memory } from '@/lib/memories';
import { BODY_RULE, bodyFits, nameFor } from '@/lib/memory-writes';

function Provenance({ memory }: { memory: Memory }) {
  return (
    <p className="text-xs text-muted-foreground">
      {memory.writtenBy === 'User' ? 'By you' : 'By Kubetail'} · <RelativeTime ms={Date.parse(memory.updatedAt)} />
    </p>
  );
}

// Edits one note's body in place, or writes a new note when there is none. The
// draft is fixed when it opens, so a change arriving meanwhile never replaces
// what the user is saving. A refused save keeps the draft and says why.
function NoteEditor({
  memory,
  onSave,
  onClose,
}: {
  memory?: Memory;
  // Resolves to why the sidecar refused, or null once saved.
  onSave: (body: string) => Promise<string | null>;
  // Called once: on a save, a discard, or a draft with nothing to save.
  onClose: () => void;
}) {
  // The body as the editor opened on it. Edits are measured against this, not
  // the live note, so a change the watch brings meanwhile never reads as one.
  const [opened] = useState(memory?.body ?? '');
  const [draft, setDraft] = useState(opened);
  const [refusal, setRefusal] = useState('');
  // The box is read-only while a save is in flight, so nothing typed after it
  // is dropped when it lands.
  const [saving, setSaving] = useState(false);
  // A ref, not state: a blur can follow a key in the same tick.
  const closed = useRef(false);
  const box = useRef<HTMLTextAreaElement>(null);
  const body = draft.trim();
  const fits = bodyFits(body);
  const unchanged = body === opened.trim();

  useEffect(() => {
    box.current?.select();
  }, []);

  const close = () => {
    closed.current = true;
    onClose();
  };

  // Only an explicit save adopts one of Kubetail's notes unchanged; a blur never does.
  const save = async (explicit: boolean) => {
    if (saving || closed.current || !fits) return;
    const adopting = explicit && memory?.writtenBy === 'Model';
    if (body === '' || (unchanged && !adopting)) {
      close();
      return;
    }
    setSaving(true);
    setRefusal('');
    const refused = await onSave(body);
    setSaving(false);
    if (refused) setRefusal(refused);
    else close();
  };

  const onKeyDown = (e: KeyboardEvent) => {
    // A key that ends an IME composition belongs to the input method.
    if (e.nativeEvent.isComposing) return;
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void save(true);
    } else if (e.key === 'Escape') {
      // Closes the editor alone, not the dialog around it. A save already
      // sent is not taken back, so Escape waits for it.
      e.stopPropagation();
      if (!saving) close();
    }
  };

  return (
    // At its minimum, a new note's row is as tall as a saved one-line note's:
    // the box's 40px takes the place of the text and its provenance line.
    <div className="flex flex-col gap-1.5 px-4 py-2.5">
      <Textarea
        prose
        ref={box}
        aria-label={memory ? 'Note' : 'New note'}
        placeholder="What should Kubetail remember?"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => void save(false)}
        readOnly={saving}
        aria-invalid={!fits || refusal !== ''}
        className="min-h-10 resize-none bg-background text-sm aria-invalid:ring-0 md:text-sm dark:bg-background"
        autoFocus
      />
      {memory && <Provenance memory={memory} />}
      {memory?.writtenBy === 'Model' && (
        <p className="text-xs text-muted-foreground">Saving makes this your note: Kubetail will follow it.</p>
      )}
      {!fits && <p className="text-xs text-destructive">{BODY_RULE}</p>}
      {refusal !== '' && <p className="text-xs text-destructive">{refusal}</p>}
    </div>
  );
}

function Row({ memory, onEdit, onDelete }: { memory: Memory; onEdit: () => void; onDelete: () => void }) {
  return (
    // Each button is named by its action and the note's body, so a screen
    // reader tells the notes apart. The trash button hides by opacity alone,
    // keeping its space and its tab stop, so focus inside the row shows it. It
    // has no fade: one would trail the pointer from row to row.
    <div className="group/row flex items-start gap-3 py-2.5 pr-3 pl-4 focus-within:bg-secondary hover:bg-secondary">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <button
          type="button"
          aria-label={`Edit note: ${memory.body}`}
          onClick={onEdit}
          className="text-left text-sm wrap-break-word whitespace-pre-wrap text-popover-foreground outline-none"
        >
          {memory.body}
        </button>
        <Provenance memory={memory} />
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={`Delete note: ${memory.body}`}
        onClick={onDelete}
        className="self-center text-muted-foreground opacity-0 transition-none group-focus-within/row:opacity-100 group-hover/row:opacity-100"
      >
        <Trash2 aria-hidden />
      </Button>
    </div>
  );
}

export function MemoryList({
  scope,
  memories,
  adding,
  onSave,
  onAddDone,
  onDelete,
}: {
  // Where the notes live, as the delete confirm names it.
  scope: string;
  memories: Memory[];
  // Whether the new-note editor is open.
  adding: boolean;
  // Resolves to why the sidecar refused, or null once saved; no memory adds one.
  onSave: (memory: Memory | undefined, name: string, body: string) => Promise<string | null>;
  onAddDone: () => void;
  // Resolves to whether the note is gone.
  onDelete: (id: string) => Promise<boolean>;
}) {
  // The notes open for editing. Starting another blurs the first, which saves
  // it, but each editor closes only once its own save lands or it is
  // discarded: a draft whose save failed stays open with its reason.
  const [editing, setEditing] = useState<ReadonlySet<string>>(new Set());
  // The id outlives the confirm's open flag, so its closing frames still name it.
  const [deleting, setDeleting] = useState({ id: '', open: false });
  const [deleteFailed, setDeleteFailed] = useState(false);
  const taken = new Set(memories.map((m) => m.name));

  const startEditing = (id: string) => setEditing((prev) => new Set(prev).add(id));
  const stopEditing = (id: string) =>
    setEditing((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });

  const closeConfirm = () => {
    setDeleting((prev) => ({ ...prev, open: false }));
    setDeleteFailed(false);
  };

  const confirmDelete = async () => {
    setDeleteFailed(false);
    if (await onDelete(deleting.id)) closeConfirm();
    else setDeleteFailed(true);
  };

  if (memories.length === 0 && !adding) {
    return <p className="text-xs text-muted-foreground">No notes yet.</p>;
  }

  return (
    <div className="flex flex-col overflow-hidden rounded-lg border">
      {adding && <NoteEditor onSave={(body) => onSave(undefined, nameFor(body, taken), body)} onClose={onAddDone} />}
      {memories.map((memory, i) => (
        <Fragment key={memory.id}>
          {(adding || i > 0) && <Separator />}
          {editing.has(memory.id) ? (
            <NoteEditor
              memory={memory}
              onSave={(body) => onSave(memory, memory.name, body)}
              onClose={() => stopEditing(memory.id)}
            />
          ) : (
            <Row
              memory={memory}
              onEdit={() => startEditing(memory.id)}
              onDelete={() => setDeleting({ id: memory.id, open: true })}
            />
          )}
        </Fragment>
      ))}
      <Dialog
        open={deleting.open}
        onOpenChange={(open) => !open && closeConfirm()}
        title="Delete note?"
        description={`The note will be permanently deleted from ${scope}. This cannot be undone.`}
      >
        {deleteFailed && <p className="text-sm text-destructive">The note could not be deleted.</p>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={closeConfirm}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirmDelete}>
            Delete note
          </Button>
        </div>
      </Dialog>
    </div>
  );
}

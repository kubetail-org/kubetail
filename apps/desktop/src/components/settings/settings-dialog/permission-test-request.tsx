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

// Test a request: the request a test would check against the user's modes and
// saved rules. Placeholder: awaits a query running permissions.Decide on a
// hypothetical request, so Test is held and no verdict is drawn.
import { Button } from '@kubetail/ui/elements/button';
import { useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';
import { Input } from '@/components/primitives/text-entry';

import { Field, Select } from './field';
import { LEVELS } from './permission-rule-form';

const VERBS = ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete', 'deletecollection'];

const optionsOf = (values: string[]) => values.map((value) => ({ value, label: value }));

export function TestRequest({ contexts }: { contexts: string[] }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button type="button" variant="outline" size="sm" onClick={() => setOpen(true)}>
        Test a request
      </Button>
      <Dialog open={open} onOpenChange={setOpen} title="Test a request" className="sm:max-w-2xl">
        <div className="flex flex-col gap-4 text-sm">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Applies to">{(id) => <Select id={id} options={LEVELS} />}</Field>
            <Field label="Context">{(id) => <Select id={id} options={optionsOf(contexts)} />}</Field>
          </div>
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] items-end gap-3">
            <Field label="Namespace">{(id) => <Input id={id} />}</Field>
            <Field label="Verb">{(id) => <Select id={id} options={optionsOf(VERBS)} />}</Field>
            <Field label="Resource">{(id) => <Input id={id} />}</Field>
            <Button type="button" disabled>
              Test
            </Button>
          </div>
          <p className="border-y py-4 text-muted-foreground">Coming soon</p>
          <p className="text-xs text-muted-foreground">
            Checks your modes and saved rules. Chat grants aren&apos;t included. Nothing is sent to the cluster.
          </p>
        </div>
      </Dialog>
    </>
  );
}

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

import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { RuleInput } from '@/lib/permission-settings';
import { expectCodeText } from '@/test-utils';

import { RuleForm } from './permission-rule-form';

const rule: RuleInput = {
  effect: 'Allow',
  accessLevel: 'Write',
  principal: null,
  target: 'Kubernetes',
  scope: 'c2/payments-*',
  service: '*',
  operation: '*',
  resource: '*',
};

const ACCOUNTS = {
  Kubernetes: [
    { value: 'c1', label: 'dev-eks' },
    { value: 'c2', label: 'Staging (staging)' },
  ],
  AWS: [{ value: '123456789012', label: '123456789012' }],
};

function renderForm(props: Partial<Parameters<typeof RuleForm>[0]> = {}) {
  const onSubmit = vi.fn();
  const onCancel = vi.fn();
  render(
    <RuleForm targets={['Kubernetes', 'AWS']} accounts={ACCOUNTS} onSubmit={onSubmit} onCancel={onCancel} {...props} />,
  );
  return { onSubmit, onCancel, form: screen.getByRole('form') };
}

const CLUSTER_SCOPED = 'Cluster-scoped objects only, instead of a namespace';

describe('RuleForm', () => {
  it("turns off the webview's suggestions in every field, each empty one reading Any", () => {
    const { form } = renderForm();
    const boxes = within(form).getAllByRole('textbox');
    expect(boxes).toHaveLength(4);
    boxes.forEach((box) => {
      expectCodeText(box);
      expect(box).toHaveAttribute('placeholder', 'Any');
    });
  });

  it('adds a cluster rule with its fields', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    expect(form).toHaveAccessibleName('Add a rule');

    await user.selectOptions(within(form).getByLabelText('Effect'), 'Deny');
    await user.selectOptions(within(form).getByLabelText('Cluster'), 'Staging (staging)');
    await user.type(within(form).getByLabelText('Group'), 'apps');
    await user.type(within(form).getByLabelText('Resource'), 'deployments');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith({
      effect: 'Deny',
      accessLevel: 'Write',
      principal: null,
      target: 'Kubernetes',
      scope: 'c2/**',
      service: 'apps',
      operation: '*',
      resource: 'deployments',
    });
  });

  it('spells the core group core and sends it empty', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    await user.type(within(form).getByLabelText('Group'), 'core');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ service: '' }));
  });

  it('keeps the account of a rule for every target it edits', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm({
      initial: { ...rule, effect: 'Deny', target: null, scope: 'prod/**' },
    });
    expect(within(form).getByLabelText('Scope')).toHaveValue('prod/**');
    await user.type(within(form).getByLabelText('Operation'), 'delete');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ target: null, scope: 'prod/**', operation: 'delete' }),
    );
  });

  it("keeps a cloud rule's empty service, drawn as none, until the field is edited", async () => {
    const user = userEvent.setup();
    const initial = { ...rule, target: 'AWS' as const, scope: '123456789012/**', service: '' };
    const { form, onSubmit } = renderForm({ initial });
    const service = within(form).getByLabelText('Service');
    expect(service).toHaveValue('');
    expect(service).toHaveAttribute('placeholder', 'None');
    await user.type(within(form).getByLabelText('Resource'), 'object');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ service: '', resource: 'object' }));

    await user.type(service, 's3');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ service: 's3' }));
  });

  it('keeps the core group of a rule for every target it edits', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm({
      initial: { ...rule, effect: 'Deny', target: null, scope: '*', service: '', resource: 'pods' },
    });
    expect(within(form).getByLabelText('Service')).toHaveValue('core');
    await user.type(within(form).getByLabelText('Operation'), 'delete');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ target: null, service: '', operation: 'delete' }));
  });

  it('cancels', async () => {
    const user = userEvent.setup();
    const { form, onCancel } = renderForm();
    await user.click(within(form).getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalled();
  });

  // The scope each choice writes: a cluster alone is it and all under it; its
  // cluster-scoped objects are the cluster alone; a namespace pattern is one
  // segment under it; every cluster is * or */pattern.
  it.each([
    ['a cluster and no namespace', 'Allow', 'dev-eks', '', false, 'c1/**'],
    ['a cluster and its cluster-scoped objects', 'Allow', 'dev-eks', '', true, 'c1'],
    ['a cluster and a namespace pattern', 'Allow', 'dev-eks', 'team-*', false, 'c1/team-*'],
    ['every cluster and no namespace', 'Deny', 'Every cluster', '', false, '*'],
    ['every cluster and a namespace pattern', 'Deny', 'Every cluster', 'kube-system', false, '*/kube-system'],
  ])('writes the scope of %s', async (_, effect, cluster, namespace, clusterScoped, scope) => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    await user.selectOptions(within(form).getByLabelText('Effect'), effect);
    await user.selectOptions(within(form).getByLabelText('Cluster'), cluster);
    if (namespace) await user.type(within(form).getByLabelText('Namespace'), namespace);
    if (clusterScoped) await user.click(within(form).getByRole('checkbox', { name: CLUSTER_SCOPED }));
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ scope }));
  });

  it("writes a cloud platform's account and place the same way", async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    await user.selectOptions(within(form).getByLabelText('Reaches'), 'AWS');
    expect(within(form).queryByRole('checkbox')).toBeNull();
    await user.type(within(form).getByLabelText('Region'), 'eu-west-1');
    await user.type(within(form).getByLabelText('Operation'), 'StartInstances');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith({
      effect: 'Allow',
      accessLevel: 'Write',
      principal: null,
      target: 'AWS',
      scope: '123456789012/eu-west-1',
      service: '*',
      operation: 'StartInstances',
      resource: '*',
    });
  });

  it('writes an account holding pattern characters as a literal', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm({ accounts: { Kubernetes: [{ value: 'dev*', label: 'dev' }] } });
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ scope: 'dev\\*/**' }));
  });

  it('shuts the namespace for cluster-scoped objects', async () => {
    const user = userEvent.setup();
    const { form } = renderForm();
    await user.type(within(form).getByLabelText('Namespace'), 'team-*');
    await user.click(within(form).getByRole('checkbox', { name: CLUSTER_SCOPED }));
    expect(within(form).getByLabelText('Namespace')).toBeDisabled();
    expect(within(form).getByLabelText('Namespace')).toHaveValue('');
    await user.click(within(form).getByRole('checkbox', { name: CLUSTER_SCOPED }));
    expect(within(form).getByLabelText('Namespace')).toBeEnabled();
  });

  it('drops cluster-scoped when the rule moves to every cluster', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Deny');
    await user.selectOptions(within(form).getByLabelText('Cluster'), 'dev-eks');
    await user.click(within(form).getByRole('checkbox', { name: CLUSTER_SCOPED }));
    await user.selectOptions(within(form).getByLabelText('Cluster'), 'Every cluster');
    await user.type(within(form).getByLabelText('Namespace'), 'kube-system');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ scope: '*/kube-system' }));
  });

  it('offers the four levels a rule may name, and no destructive one to an Allow', async () => {
    const user = userEvent.setup();
    const { form } = renderForm();
    const names = () =>
      within(within(form).getByLabelText('Applies to'))
        .getAllByRole('option')
        .map((o) => o.textContent);
    expect(names()).toEqual(['Writes', 'Data reads', 'Access control changes']);
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Deny');
    expect(names()).toEqual(['Writes', 'Data reads', 'Access control changes', 'Destructive changes']);
  });

  it('falls back to writes when an Allow no longer offers the level picked', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Deny');
    await user.selectOptions(within(form).getByLabelText('Applies to'), 'Destructive changes');
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Allow');
    expect(within(form).getByLabelText('Applies to')).toHaveValue('Write');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ effect: 'Allow', accessLevel: 'Write' }));
  });

  // Anyone by default; a chat or a subagent, never the monitor or the
  // console, and an Allow for a subagent never.
  it('names a principal, anyone by default', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    const names = () =>
      within(within(form).getByLabelText('Sent by'))
        .getAllByRole('option')
        .map((o) => o.textContent);
    expect(within(form).getByLabelText('Sent by')).toHaveValue('');
    expect(names()).toEqual(['Anyone', 'A chat']);
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Deny');
    expect(names()).toEqual(['Anyone', 'A chat', 'A subagent']);
    await user.selectOptions(within(form).getByLabelText('Sent by'), 'A subagent');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ principal: 'Subagent' }));
  });

  // Adding a rule the always rules already hold writes nothing, so saving it
  // unchanged would leave the remove to delete the original.
  it('edits a rule from its fields, saving only once the draft differs', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm({ initial: rule });
    expect(form).toHaveAccessibleName('Edit rule');
    expect(within(form).getByLabelText('Cluster')).toHaveValue('c2');
    expect(within(form).getByLabelText('Namespace')).toHaveValue('payments-*');
    const save = within(form).getByRole('button', { name: 'Save' });
    expect(save).toBeDisabled();

    await user.type(within(form).getByLabelText('Verb'), 'patch');
    expect(save).toBeEnabled();
    await user.clear(within(form).getByLabelText('Verb'));
    expect(save).toBeDisabled();

    await user.selectOptions(within(form).getByLabelText('Effect'), 'Ask');
    await user.click(save);
    expect(onSubmit).toHaveBeenCalledWith({ ...rule, effect: 'Ask' });
  });

  it('offers the account of a rule it edits though no mode lists it', async () => {
    const user = userEvent.setup();
    const { onSubmit, form } = renderForm({
      initial: { ...rule, target: 'AWS', scope: '210987654321/**' },
    });
    const account = within(form).getByLabelText('Account');
    expect(account).toHaveValue('210987654321');
    expect(within(account).getByRole('option', { name: '210987654321' })).toBeInTheDocument();
    await user.type(within(form).getByLabelText('Resource'), 'object');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ target: 'AWS', scope: '210987654321/**' }));
  });

  it('keeps a scope it cannot draw as it was', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm({ initial: { ...rule, effect: 'Deny', scope: '**' } });
    expect(within(form).getByLabelText('Scope')).toHaveValue('**');
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Ask');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ effect: 'Ask', scope: '**' }));
  });

  // An Allow for every cluster would reach a cluster added later, so only
  // Deny and Ask offer it, and an Allow falls back to the first cluster.
  it('offers every cluster and every target to Deny and Ask alone', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm();
    const every = () => within(form).queryByRole('option', { name: 'Every cluster' });

    expect(every()).toBeNull();
    expect(within(form).queryByRole('option', { name: 'Every target' })).toBeNull();
    expect(within(form).getByLabelText('Cluster')).toHaveValue('c1');
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Ask');
    await user.selectOptions(within(form).getByLabelText('Cluster'), 'Every cluster');
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ effect: 'Ask', scope: '*' }));

    await user.selectOptions(within(form).getByLabelText('Reaches'), 'Every target');
    expect(within(form).getByLabelText('Account')).toBeDisabled();
    await user.click(within(form).getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ target: null, scope: '*' }));

    await user.selectOptions(within(form).getByLabelText('Effect'), 'Allow');
    expect(within(form).getByLabelText('Reaches')).toHaveValue('Kubernetes');
  });

  it('adds no Allow while there is no account to name', () => {
    const { form } = renderForm({ accounts: {} });
    expect(within(form).getByRole('button', { name: 'Add' })).toBeDisabled();
  });

  it('narrows a cloud rule of its account alone to a place, keeping the account', async () => {
    const user = userEvent.setup();
    const { form, onSubmit } = renderForm({
      targets: ['Kubernetes', 'AWS', 'GitHub'],
      accounts: { ...ACCOUNTS, GitHub: [{ value: 'kubetail-org', label: 'kubetail-org' }] },
      initial: { ...rule, target: 'GitHub', scope: 'kubetail-org' },
    });
    const only = within(form).getByRole('checkbox', { name: 'The owner itself only, instead of a repository' });
    expect(only).toBeChecked();
    expect(within(form).getByLabelText('Repository')).toBeDisabled();
    await user.click(only);
    await user.type(within(form).getByLabelText('Repository'), 'kubetail');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ target: 'GitHub', scope: 'kubetail-org/kubetail' }),
    );
  });

  it('offers no account-only scope on AWS, whose every action names a region', async () => {
    const user = userEvent.setup();
    const { form } = renderForm();
    await user.selectOptions(within(form).getByLabelText('Reaches'), 'AWS');
    expect(within(form).queryByRole('checkbox')).toBeNull();
  });

  it('opens a cluster-scoped rule with its box checked', () => {
    const { form } = renderForm({ initial: { ...rule, scope: 'c2' } });
    expect(within(form).getByRole('checkbox', { name: CLUSTER_SCOPED })).toBeChecked();
  });

  it('shows the core group and a named resource of a rule it edits', () => {
    const { form } = renderForm({ initial: { ...rule, service: '', operation: 'get', resource: 'pods' } });
    expect(within(form).getByLabelText('Group')).toHaveValue('core');
    expect(within(form).getByLabelText('Verb')).toHaveValue('get');
    expect(within(form).getByLabelText('Resource')).toHaveValue('pods');
  });

  it('drops a subagent once the effect turns to Allow, and goes back to anyone', async () => {
    const user = userEvent.setup();
    const { form } = renderForm({ initial: { ...rule, effect: 'Deny', principal: 'Subagent' } });
    const sentBy = within(form).getByLabelText('Sent by');
    expect(sentBy).toHaveValue('Subagent');
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Allow');
    expect(sentBy).toHaveValue('');
    await user.selectOptions(sentBy, 'A chat');
    expect(sentBy).toHaveValue('Chat');
    await user.selectOptions(sentBy, 'Anyone');
    expect(sentBy).toHaveValue('');
  });

  it('names no target for an Allow while the machine offers none', async () => {
    const user = userEvent.setup();
    const { form } = renderForm({ targets: [] });
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Deny');
    await user.selectOptions(within(form).getByLabelText('Reaches'), 'Every target');
    await user.selectOptions(within(form).getByLabelText('Effect'), 'Allow');
    expect(within(form).getByRole('button', { name: 'Add' })).toBeDisabled();
  });
});

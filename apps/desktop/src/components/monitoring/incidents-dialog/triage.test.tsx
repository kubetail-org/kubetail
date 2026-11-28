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
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Incident, IncidentTriage } from '@/lib/incidents';
import type { Model } from '@/lib/models';
import { incidentRecord, monitorTargetRecord } from '@/test-utils';

// What the mocked hooks answer; `sandbox` is undefined until the sidecar says.
type State = { models: Model[]; sandbox: boolean | undefined; sending: Set<string>; errors: Map<string, string> };

const { state, triage } = vi.hoisted(() => {
  const initial: State = { models: [], sandbox: true, sending: new Set(), errors: new Map() };
  return { state: initial, triage: vi.fn(async () => {}) };
});

vi.mock('@/lib/models', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/models')>()),
  useModels: () => ({ models: state.models, snapshotComplete: true }),
}));
vi.mock('@/lib/sandbox', () => ({ useSandbox: () => ({ available: state.sandbox }) }));
vi.mock('@/lib/incidents', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/incidents')>()),
  useIncidentTriage: () => ({ sending: state.sending, errors: state.errors, triage }),
}));

const { IncidentTriageSection } = await import('./triage');

const model = (providerID: string, id: string): Model => ({
  provider: { id: providerID, label: providerID },
  id,
  label: id,
  efforts: ['low', 'high'],
  defaultEffort: 'low',
});

const target = monitorTargetRecord();
const none = incidentRecord().triage;
const withTriage = (t: Partial<IncidentTriage>): Incident => incidentRecord({ triage: { ...none, ...t } });

const action = () => screen.queryByRole('button', { name: /Triage/ });

beforeEach(() => {
  vi.clearAllMocks();
  state.models = [model('anthropic', 'claude'), model('openai', 'gpt')];
  state.sandbox = true;
  state.sending = new Set();
  state.errors = new Map();
});

describe('IncidentTriageSection', () => {
  it('asks for a triage of the incident on the model picked', async () => {
    render(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    await userEvent.click(screen.getByRole('button', { name: 'Triage with AI' }));
    expect(triage).toHaveBeenCalledWith('i1', { model: { providerID: 'anthropic', id: 'claude' }, effort: 'low' });
  });

  it('keeps a triage that ended once its target moves, offering it no more', () => {
    const failed = withTriage({ state: 'Failed', error: 'ServerChanged' });
    render(<IncidentTriageSection incident={failed} target={{ ...target, currentUID: 'uid-2' }} />);
    expect(action()).not.toBeInTheDocument();
    expect(screen.getByText(/another server than the one this incident opened on/)).toBeInTheDocument();
  });

  it('is offered only for a target that can triage and still reaches the incident’s server', () => {
    const view = render(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    expect(action()).toBeInTheDocument();

    view.rerender(<IncidentTriageSection incident={incidentRecord()} target={{ ...target, currentUID: 'uid-2' }} />);
    expect(action()).not.toBeInTheDocument();

    view.rerender(<IncidentTriageSection incident={incidentRecord()} target={{ ...target, canTriage: false }} />);
    expect(action()).not.toBeInTheDocument();
  });

  it('is disabled, saying why, with no sandbox, monitoring off or no provider signed in', () => {
    state.sandbox = false;
    const view = render(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    expect(action()).toBeDisabled();
    expect(screen.getByText(/needs a sandbox/)).toBeInTheDocument();

    state.sandbox = true;
    view.rerender(<IncidentTriageSection incident={incidentRecord()} target={{ ...target, enabled: false }} />);
    expect(action()).toBeDisabled();
    expect(screen.getByText(/Monitoring of this target is off/)).toBeInTheDocument();

    state.models = [];
    view.rerender(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    expect(action()).toBeDisabled();
    expect(screen.getByText('No model provider is signed in.')).toBeInTheDocument();
  });

  it('is disabled while the sandbox is unanswered', () => {
    state.sandbox = undefined;
    render(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    expect(action()).toBeDisabled();
  });

  it('is disabled once the picked model’s provider signs out', async () => {
    const view = render(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    await userEvent.click(screen.getByRole('button', { name: /^Model:/ }));
    await userEvent.click(screen.getByRole('button', { name: 'openai' }));
    await userEvent.click(screen.getByRole('radio', { name: 'gpt' }));

    state.models = [model('anthropic', 'claude')];
    view.rerender(<IncidentTriageSection incident={incidentRecord()} target={target} />);

    expect(action()).toBeDisabled();
    expect(screen.getByText("The model's provider is signed out.")).toBeInTheDocument();
  });

  it('draws a queued triage under way, and one being sent', () => {
    const view = render(<IncidentTriageSection incident={withTriage({ state: 'Queued' })} target={target} />);
    expect(screen.getByRole('button', { name: 'Triage under way…' })).toBeDisabled();

    state.sending = new Set(['i1']);
    view.rerender(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    expect(screen.getByRole('button', { name: 'Triage under way…' })).toBeDisabled();
  });

  it('draws a done triage’s verdict, model and report as plain text', () => {
    const report = 'ATTENTION\n<b>bold</b> [a link](https://example.com) **strong**\u001b[31m red';
    render(
      <IncidentTriageSection
        incident={withTriage({
          state: 'Done',
          verdict: 'Attention',
          report,
          providerID: 'anthropic',
          modelID: 'claude',
          effort: 'high',
        })}
        target={target}
      />,
    );
    expect(screen.getByText('Verdict: needs attention')).toBeInTheDocument();
    expect(
      screen.getByText((_, el) => el?.textContent === 'Ran on claude, effort high' && el.tagName === 'SPAN'),
    ).toBeInTheDocument();
    const drawn = screen.getByLabelText('Report');
    expect(drawn).toHaveTextContent('<b>bold</b> [a link](https://example.com) **strong**');
    expect(drawn.querySelector('b, a, strong')).toBeNull();
    expect(within(drawn).getByTitle('An invisible character')).toBeInTheDocument();
    expect(drawn.textContent).not.toContain('\u001b');
    expect(screen.getByRole('button', { name: 'Triage with AI' })).toBeEnabled();
  });

  it('draws a dismissed verdict', () => {
    render(
      <IncidentTriageSection
        incident={withTriage({ state: 'Done', verdict: 'Dismiss', report: 'DISMISS' })}
        target={target}
      />,
    );
    expect(screen.getByText('Verdict: nothing to do')).toBeInTheDocument();
  });

  it('says why a triage failed, and offers it again', () => {
    render(<IncidentTriageSection incident={withTriage({ state: 'Failed', error: 'RunFailed' })} target={target} />);
    expect(screen.getByText('The model run failed.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Triage with AI' })).toBeEnabled();
  });

  it('shows a refused request as visible text', () => {
    state.errors = new Map([['i1', 'Validation \u202eerror']]);
    render(<IncidentTriageSection incident={incidentRecord()} target={target} />);
    const drawn = screen.getByText((_, el) => el?.tagName === 'P' && el.textContent.startsWith('Validation'));
    expect(within(drawn).getByTitle('An invisible character')).toBeInTheDocument();
    expect(drawn.textContent).not.toContain('\u202e');
  });
});

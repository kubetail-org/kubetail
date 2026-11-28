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

// An incident's triage: the action that asks for one on a model the user picks, with
// the box that leaves automatic triage on with it, and the newest triage as the watch
// delivers it. The report is model output over cluster text, drawn as plain text,
// never as Markdown.
import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { cn } from '@kubetail/ui/lib/utils';
import { useState } from 'react';

import { ModelPicker } from '@/components/primitives/model-picker';
import { VisibleText } from '@/components/primitives/visible-text';
import { TONE } from '@/lib/cluster-health';
import { useDialog } from '@/lib/dialog';
import { TRIAGE_ERROR_TEXT, useIncidentTriage, useMonitoringTriageModel } from '@/lib/incidents';
import type { Incident, IncidentTriage, MonitoringTriageModel, MonitorTarget } from '@/lib/incidents';
import { modelOf, seedPick, useModels } from '@/lib/models';
import type { ModelPick } from '@/lib/models';
import { useSandbox } from '@/lib/sandbox';

/**
 * The action is offered for an incident whose target can triage and still reaches
 * the server the incident opened on, both read from the target, so a target that
 * moves takes it away. A triage that ended stays drawn either way.
 */
export function IncidentTriageSection({ incident, target }: { incident: Incident; target: MonitorTarget }) {
  const offered = target.canTriage && incident.targetUID === target.currentUID;
  const ended = incident.triage.state === 'Done' || incident.triage.state === 'Failed';
  if (!offered && !ended) return null;
  return (
    <section aria-label="Triage" className="flex flex-col gap-2">
      <h4 className="text-xs font-medium text-muted-foreground">Triage</h4>
      {offered && <TriageAction incident={incident} target={target} />}
      <TriageOutcome triage={incident.triage} />
    </section>
  );
}

function TriageAction({ incident, target }: { incident: Incident; target: MonitorTarget }) {
  const { models } = useModels();
  const sandbox = useSandbox();
  const { sending, errors, triage } = useIncidentTriage();
  const setting = useMonitoringTriageModel();
  const [chosen, setChosen] = useState<ModelPick>();
  // Ticking writes nothing: the box rides the press, so closing the picker leaves the
  // setting. It is ticked for one pick and one press: another pick, or a press, unticks it.
  const [alwaysFor, setAlwaysFor] = useState<ModelPick | null>(null);
  const pick = chosen ?? seedPick(models, null);
  const always = !!pick && !!alwaysFor && samePick(alwaysFor, pick);
  const picked = pick ? modelOf(models, pick.model) : null;
  const alreadyOn = !!pick && !!setting && namesPick(setting, pick);
  const underWay = incident.triage.state === 'Queued' || sending.has(incident.id);

  let blocked: string | null = null;
  if (sandbox.available === false) blocked = 'Triage needs a sandbox, which this machine does not have.';
  else if (!target.enabled) blocked = 'Monitoring of this target is off. Turn it on to triage its incidents.';
  else if (!pick) blocked = 'No model provider is signed in.';
  else if (!picked) blocked = "The model's provider is signed out.";
  const refusal = errors.get(incident.id);
  // Undefined with no pick, when the button is disabled.
  const press =
    pick &&
    (() => {
      setAlwaysFor(null);
      void triage(incident.id, pick, always && !alreadyOn);
    });

  return (
    <>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          size="sm"
          disabled={underWay || blocked !== null || sandbox.available === undefined}
          onClick={press}
        >
          {underWay ? 'Triage under way…' : 'Triage with AI'}
        </Button>
        {pick && models.length > 0 && <ModelPicker models={models} pick={pick} picked={picked} onPick={setChosen} />}
      </div>
      {pick && setting !== undefined && (
        <AlwaysTriage on={alreadyOn} checked={always} onCheck={(v) => setAlwaysFor(v ? pick : null)} />
      )}
      {blocked && <p className="text-xs text-muted-foreground">{blocked}</p>}
      {refusal && (
        <p className="text-xs text-destructive">
          <VisibleText text={refusal} />
        </p>
      )}
    </>
  );
}

function samePick(a: ModelPick, b: ModelPick): boolean {
  return a.model.providerID === b.model.providerID && a.model.id === b.model.id && a.effort === b.effort;
}

// Whether the setting is the model picked, effort included.
function namesPick(setting: MonitoringTriageModel, pick: ModelPick): boolean {
  return (
    setting.providerID === pick.model.providerID &&
    setting.modelID === pick.model.id &&
    (setting.effort ?? '') === pick.effort
  );
}

// The box that turns automatic triage on with the press, or, while the setting already
// names the model picked, the line that says so, linked to the Monitoring tab.
function AlwaysTriage({ on, checked, onCheck }: { on: boolean; checked: boolean; onCheck: (v: boolean) => void }) {
  const { openDialog } = useDialog();
  if (on) {
    return (
      <p className="text-xs text-muted-foreground">
        New incidents are triaged with this model.{' '}
        <Button
          type="button"
          variant="link"
          size="sm"
          className="h-auto p-0 text-xs"
          onClick={() => openDialog('settings', 'Monitoring')}
        >
          Monitoring settings
        </Button>
      </p>
    );
  }
  return (
    // oxlint-disable-next-line jsx-a11y/label-has-associated-control -- Checkbox renders its control inside
    <label className="flex items-center gap-2 text-xs">
      <Checkbox checked={checked} onCheckedChange={onCheck} />
      Always triage new incidents with this model
    </label>
  );
}

// The newest triage once it has ended: its verdict and report, or why it failed.
function TriageOutcome({ triage }: { triage: IncidentTriage }) {
  if (triage.state !== 'Done' && triage.state !== 'Failed') return null;
  const dismissed = triage.verdict === 'Dismiss';
  return (
    <div className="flex flex-col gap-1 rounded-md border px-3 py-2 text-xs">
      {triage.state === 'Done' ? (
        <span className={cn('font-medium', dismissed ? 'text-muted-foreground' : TONE.attention.text)}>
          {dismissed ? 'Verdict: nothing to do' : 'Verdict: needs attention'}
        </span>
      ) : (
        <span className="text-destructive">{triage.error && TRIAGE_ERROR_TEXT[triage.error]}</span>
      )}
      {triage.modelID && (
        <span className="text-muted-foreground">
          Ran on <VisibleText text={triage.modelID} />
          {triage.effort && <>, effort {triage.effort}</>}
        </span>
      )}
      {triage.report && (
        <p aria-label="Report" className="font-mono whitespace-pre-wrap">
          <VisibleText text={triage.report} />
        </p>
      )}
    </div>
  );
}

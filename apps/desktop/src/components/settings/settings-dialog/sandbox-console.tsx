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

// The Sandbox console: a command run in a fresh sandboxed workspace, as a
// chat's would be, with the cluster and network the user picks. Its output is
// attacker-controlled text: control sequences stripped, drawn through
// VisibleText. Closing the console stops a command still running.
import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { stripAnsi } from 'fancy-ansi';
import { SquareTerminal, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';

import { DialogClose, DialogTitle, PanelDialog } from '@/components/primitives/dialog';
import { Input } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import { EVERY, useScope } from '@/lib/cluster-scope';
import { displayName } from '@/lib/clusters';
import { humanDuration } from '@/lib/human-duration';
import { type ConsoleRun, useSandboxConsole } from '@/lib/sandbox-console';

const EXAMPLES = ['ls ~', 'cat ~/.ssh/config', 'curl -sI https://example.com', 'kubectl get ns'];

type SandboxConsoleProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  networkAvailable: boolean | undefined;
};

export function SandboxConsole({ open, onOpenChange, networkAvailable }: SandboxConsoleProps) {
  const { present } = useScope(EVERY);
  const runner = useSandboxConsole(open);
  const [cluster, setCluster] = useState<string | null>(null);
  const [network, setNetwork] = useState(false);
  const [command, setCommand] = useState('');
  const names = new Map(present.map((c) => [c.id, displayName(c)]));
  // The picked cluster can be missing for a moment, while the clusters watch
  // reconnects; nothing runs then, rather than run with no cluster.
  const clusterListed = cluster === null || names.has(cluster);
  const canRun = !runner.running && clusterListed && command.trim() !== '';

  const run = async () => {
    // Enter submits whatever the button shows, so it is guarded here too.
    if (!canRun) return;
    const sent = command;
    if (await runner.run(sent, cluster, network && networkAvailable === true)) {
      // Keeps what was typed while the run was asked for.
      setCommand((current) => (current === sent ? '' : current));
    }
  };
  const changeOpen = (next: boolean) => {
    // Always, whatever is drawn: a run can be asked for while the watch has not
    // shown it, and a stop with nothing running does nothing.
    /* v8 ignore next -- the dialog has no trigger, so it only ever asks to close */
    if (!next) runner.stop();
    onOpenChange(next);
  };

  return (
    <PanelDialog open={open} onOpenChange={changeOpen} className="h-140 w-200 flex-col bg-popover">
      <div className="flex items-center border-b px-4 py-2">
        <DialogTitle className="flex-1 text-center text-sm font-medium">Sandbox console</DialogTitle>
        <DialogClose aria-label="Close" render={<Button variant="ghost" size="icon" className="size-7" />}>
          <X className="size-4" />
        </DialogClose>
      </div>
      <div className="flex items-center gap-3 border-b px-4 py-2 text-sm">
        <span className="text-muted-foreground">Cluster</span>
        <Select
          value={cluster}
          onValueChange={(next, { reason }) => {
            // Base UI clears the value when its item leaves the list, as the
            // clusters watch reconnecting does; the pick is the user's to change.
            if (next !== null || reason !== 'none') setCluster(next);
          }}
        >
          <SelectTrigger size="sm" className="w-48" aria-label="Cluster">
            <SelectValue>
              {/* From state: Base UI hands over null for a value no item holds. */}
              <VisibleText text={cluster === null ? 'No cluster' : (names.get(cluster) ?? cluster)} trailing="end" />
            </SelectValue>
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectItem value={null}>No cluster</SelectItem>
            {present.map((c) => (
              <SelectItem key={c.id} value={c.id}>
                <VisibleText text={displayName(c)} trailing="end" />
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {/* oxlint-disable-next-line jsx-a11y/label-has-associated-control -- Checkbox renders its control inside */}
        <label className="flex items-center gap-2">
          <Checkbox checked={network} disabled={!networkAvailable} onCheckedChange={setNetwork} />
          Network
        </label>
        <span className="ml-auto text-xs text-muted-foreground">Your always grants apply</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {runner.runs.length > 0 ? <Runs runs={runner.runs} /> : <Empty onPick={setCommand} />}
      </div>
      <form
        className="flex flex-col gap-2 border-t px-4 py-3"
        onSubmit={(e) => {
          e.preventDefault();
          void run();
        }}
      >
        <div className="flex gap-2">
          <Input
            aria-label="Command"
            className="flex-1 font-mono"
            placeholder="Type a command"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
          />
          {runner.running ? (
            <Button type="button" variant="outline" onClick={runner.stop}>
              Stop
            </Button>
          ) : (
            <Button type="submit" disabled={!canRun}>
              Run
            </Button>
          )}
        </div>
        {runner.error && (
          <p role="alert" className="text-xs text-destructive">
            <VisibleText text={runner.error} />
          </p>
        )}
        <p className="text-xs text-muted-foreground">
          Runs in a workspace of its own, as a chat&apos;s command would, and changes no cluster. Nothing here reaches a
          chat or the model.
        </p>
      </form>
    </PanelDialog>
  );
}

function Empty({ onPick }: { onPick: (command: string) => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <div className="flex size-10 items-center justify-center rounded-md bg-muted">
        <SquareTerminal className="size-5" aria-hidden />
      </div>
      <p className="text-sm font-medium">Try a command in the sandbox</p>
      <p className="max-w-md text-xs text-muted-foreground">
        It runs the way a chat&apos;s command does: your always-granted folders, no network unless you tick Network, and
        only the cluster you pick, read but never changed.
      </p>
      <p className="text-xs text-muted-foreground">Try one</p>
      <div className="flex flex-wrap justify-center gap-2">
        {EXAMPLES.map((example) => (
          <Button key={example} variant="outline" size="sm" className="font-mono" onClick={() => onPick(example)}>
            {example}
          </Button>
        ))}
      </div>
    </div>
  );
}

function Runs({ runs }: { runs: ConsoleRun[] }) {
  const end = useRef<HTMLDivElement>(null);
  const last = runs.at(-1);
  // Keeps the newest output in view as it arrives.
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: 'end' });
  }, [last?.number, last?.output, last?.running]);
  return (
    <>
      <ul className="flex flex-col gap-4 font-mono text-xs">
        {runs.map((run) => (
          <li key={run.number} className="flex flex-col gap-1">
            <p className="text-sm break-all whitespace-pre-wrap">
              <span className="text-muted-foreground">$ </span>
              <VisibleText text={run.command} trailing="end" />
            </p>
            {run.output !== '' && (
              <p className="break-all whitespace-pre-wrap" data-testid="output">
                <VisibleText text={stripAnsi(run.output)} trailing="end" />
              </p>
            )}
            {run.discarded > 0 && <p className="text-muted-foreground">{`… ${run.discarded} more bytes not shown`}</p>}
            <RunEnd run={run} />
          </li>
        ))}
      </ul>
      <div ref={end} />
    </>
  );
}

function RunEnd({ run }: { run: ConsoleRun }) {
  if (run.running) return <p className="text-muted-foreground">Running…</p>;
  const duration = run.durationMs < 1000 ? `${run.durationMs}ms` : humanDuration(run.durationMs);
  const outcome = run.notice ?? (run.exitCode === null ? 'ended' : `exit ${run.exitCode}`);
  return (
    <p className={run.exitCode !== 0 ? 'text-destructive' : 'text-muted-foreground'}>
      <VisibleText text={`${outcome} · ${duration}`} trailing="end" />
    </p>
  );
}

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

// The Sandbox console: the user's commands run in the sandbox, as a chat's
// would. The one reader of the console's watch and mutations. A run answers
// once the command starts; the watch carries its output and its end. It is
// a state watch of the last command alone, so the history is folded from
// its frames by each command's number, and starts over on a reconnect.
import { useCallback, useEffect, useRef, useState } from 'react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import type { SandboxConsoleStateWatchSubscription } from '@/gql/graphql';
import { refusalOf } from '@/lib/graphql/refusal';
import { perFrame, useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

const SandboxConsoleStateWatchDocument = graphql(`
  subscription SandboxConsoleStateWatch {
    sandboxConsoleStateWatch {
      run {
        number
        command
        clusterID
        network
        running
        output
        discarded
        exitCode
        notice
        durationMs
      }
    }
  }
`);

const SandboxConsoleRunMutation = graphql(`
  mutation SandboxConsoleRun($command: String!, $clusterID: ClusterID, $network: Boolean!) {
    sandboxConsoleRun(command: $command, clusterID: $clusterID, network: $network)
  }
`);

const SandboxConsoleStopMutation = graphql(`
  mutation SandboxConsoleStop($number: Int) {
    sandboxConsoleStop(number: $number)
  }
`);

export type ConsoleRun = NonNullable<SandboxConsoleStateWatchSubscription['sandboxConsoleStateWatch']['run']>;

const NO_RUNS: ConsoleRun[] = [];

// Each frame replaces its command's entry, or adds it at the end.
const foldRuns = perFrame<SandboxConsoleStateWatchSubscription, ConsoleRun[]>((prev, data) => {
  const runs = prev ?? NO_RUNS;
  const { run } = data.sandboxConsoleStateWatch;
  if (!run) return runs;
  const at = runs.findIndex((r) => r.number === run.number);
  return at === -1 ? [...runs, run] : runs.with(at, run);
});

export type SandboxConsoleState = {
  /** Each command seen since the watch opened, oldest first, the last one perhaps still running. */
  runs: ConsoleRun[];
  /** A command runs, or one has been asked for and the watch has not shown it yet. */
  running: boolean;
  /** The last run's refusal, until the next run. */
  error: string | null;
  /** Runs command; true once it started, false when refused or while another run is asked for. */
  run: (command: string, clusterID: string | null, network: boolean) => Promise<boolean>;
  stop: () => void;
};

/** The console, its watch open while `open`. */
export function useSandboxConsole(open: boolean): SandboxConsoleState {
  const { data, generation } = useFoldedSubscription(
    { query: SandboxConsoleStateWatchDocument, pause: !open },
    foldRuns,
  );
  const [, runMutation] = useMutation(SandboxConsoleRunMutation);
  const [, stopMutation] = useMutation(SandboxConsoleStopMutation);
  const [error, setError] = useState<string | null>(null);
  // A refusal does not outlive the console it was shown in.
  if (!open && error !== null) setError(null);
  const runs = data ?? NO_RUNS;
  const last = runs.at(-1);

  // The mutation's answer and the watch's frame arrive in no fixed order, so a
  // run counts as running while it is asked for, then until the watch shows its
  // number. Numbers start over with a restarted sidecar, so a new connection
  // drops the wait.
  const [asking, setAsking] = useState(false);
  const [started, setStarted] = useState<{ number: number; generation: number } | null>(null);
  const shown = started !== null && ((last?.number ?? 0) >= started.number || generation !== started.generation);
  if (shown) setStarted(null);
  const running = asking || (started !== null && !shown) || (last?.running ?? false);

  // A stop sent while the run is asked for can reach the sidecar before the
  // command starts, and stop nothing; the run sends it again, by number, once
  // the command starts. own is the last command this console started.
  const pending = useRef<{ stopped: boolean } | null>(null);
  const own = useRef<number | null>(null);

  const run = useCallback(
    async (command: string, clusterID: string | null, network: boolean) => {
      if (pending.current) return false;
      const request = { stopped: false };
      pending.current = request;
      setAsking(true);
      const { data: answer, error: refused } = await runMutation({ command, clusterID, network });
      pending.current = null;
      setAsking(false);
      setError(refusalOf(refused));
      if (!answer) return false;
      const number = answer.sandboxConsoleRun;
      own.current = number;
      setStarted({ number, generation });
      if (request.stopped) void stopMutation({ number });
      return true;
    },
    [runMutation, stopMutation, generation],
  );

  const stop = useCallback(() => {
    if (pending.current) pending.current.stopped = true;
    void stopMutation({ number: null });
  }, [stopMutation]);

  // Unmounted some other way than a close (the sandbox gone, the window
  // closed), the console stops its own command: a stop by number ends nothing
  // else.
  useEffect(
    () => () => {
      if (pending.current) pending.current.stopped = true;
      else if (own.current !== null) void stopMutation({ number: own.current });
    },
    [stopMutation],
  );

  return { runs, running, error, run, stop };
}

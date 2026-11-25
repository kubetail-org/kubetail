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

import type {
  AWSConnection,
  ChatGPTConnection,
  GitHubConnection,
  GoogleConnection,
  OpenRouterConnection,
} from './connectors';

// The seams: each watch's frames by the subscription its document names, folded
// by the hook's own reducer, and each mutation, by the operation its document names.
const { watch, mutations } = vi.hoisted(() => ({
  watch: { frames: {} } as { frames: Record<string, unknown[]>; options?: unknown },
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
}));
vi.mock('@/lib/graphql/use-folded-subscription', () => ({
  perFrame:
    <D, R>(step: (prev: R | undefined, frame: D) => R) =>
    (prev: R | undefined, frames: D[]) =>
      frames.reduce<R | undefined>(step, prev),
  useFoldedSubscription: (
    args: { query: { name: string } },
    reduce: (prev: unknown, frames: unknown[]) => unknown,
    options: unknown,
  ) => {
    watch.options = options;
    const frames = watch.frames[args.query.name];
    return { data: frames ? reduce(undefined, frames) : undefined, connected: true };
  },
}));
vi.mock('@/gql', () => ({
  graphql: (source: string) => ({ name: /(?:mutation|subscription) (\w+)/.exec(source)![1] }),
}));
vi.mock('urql', () => ({
  useMutation: (doc: { name: string }) => {
    mutations[doc.name] ??= vi.fn();
    return [{}, mutations[doc.name]];
  },
}));

const { useConnectors, useOpenChatGPTUsage } = await import('./connectors');

function connection(c: Partial<ChatGPTConnection>): ChatGPTConnection {
  return { phase: 'SignedOut', email: null, failure: null, registered: false, revocationUnconfirmed: false, ...c };
}

function openRouter(c: Partial<OpenRouterConnection> = {}): OpenRouterConnection {
  return { phase: 'SignedOut', failure: null, ...c };
}

function aws(c: Partial<AWSConnection> = {}): AWSConnection {
  return { phase: 'SignedOut', arn: null, failure: null, ...c };
}

function gitHub(c: Partial<GitHubConnection> = {}): GitHubConnection {
  return { phase: 'SignedOut', login: null, userCode: null, failure: null, ...c };
}

function google(c: Partial<GoogleConnection> = {}): GoogleConnection {
  return { phase: 'SignedOut', email: null, project: null, failure: null, revocationUnconfirmed: false, ...c };
}

// Serves every watch's frames: ChatGPT's, then OpenRouter's, AWS's, GitHub's and
// Google's, which default to signed out.
function serve(
  chatgpt: ChatGPTConnection[],
  openrouter: OpenRouterConnection[] = [openRouter()],
  awsFrames: AWSConnection[] = [aws()],
  github: GitHubConnection[] = [gitHub()],
  googleFrames: GoogleConnection[] = [google()],
) {
  watch.frames = {
    ChatGPTConnectionStateWatch: chatgpt.map((c) => ({ chatGPTConnectionStateWatch: c })),
    OpenRouterConnectionStateWatch: openrouter.map((c) => ({ openRouterConnectionStateWatch: c })),
    AWSConnectionStateWatch: awsFrames.map((c) => ({ awsConnectionStateWatch: c })),
    GitHubConnectionStateWatch: github.map((c) => ({ gitHubConnectionStateWatch: c })),
    GoogleConnectionStateWatch: googleFrames.map((c) => ({ googleConnectionStateWatch: c })),
  };
}

beforeEach(() => {
  watch.frames = {};
  for (const k of Object.keys(mutations)) delete mutations[k];
});

describe('useConnectors', () => {
  it('lists the planned connectors and has not loaded before the first frame', () => {
    const { result } = renderHook(() => useConnectors());

    expect(result.current.snapshotComplete).toBe(false);
    expect(result.current.connectors).toEqual([
      { id: 'github', name: 'GitHub', detail: 'Repositories for git and gh in the sandbox', connected: false },
      { id: 'aws', name: 'AWS', detail: 'Cloud resources and Bedrock models', connected: false },
      { id: 'google', name: 'Google', detail: 'Gemini models on Vertex AI', connected: false },
      { id: 'azure', name: 'Azure', detail: 'Cloud resources and Foundry models', connected: false },
      { id: 'chatgpt', name: 'ChatGPT', detail: 'OpenAI models on your ChatGPT plan', connected: false },
      { id: 'openrouter', name: 'OpenRouter', detail: 'Many providers on one account', connected: false },
      { id: 'gitlab', name: 'GitLab', detail: 'Code, pipelines and the container registry', connected: false },
      {
        id: 'atlassian',
        name: 'Atlassian',
        detail: 'Jira issues, Confluence pages and Opsgenie alerts',
        connected: false,
      },
      { id: 'linear', name: 'Linear', detail: 'Issues and projects', connected: false },
      { id: 'datadog', name: 'Datadog', detail: 'Metrics, logs, traces and monitors', connected: false },
      { id: 'grafana', name: 'Grafana', detail: 'Dashboards, alerts, Loki logs and Mimir metrics', connected: false },
      { id: 'victoriametrics', name: 'VictoriaMetrics', detail: 'Metrics and logs', connected: false },
      { id: 'clickhouse', name: 'ClickHouse', detail: 'Queries over your ClickHouse Cloud data', connected: false },
      { id: 'newrelic', name: 'New Relic', detail: 'Metrics, logs, traces and alerts', connected: false },
      { id: 'honeycomb', name: 'Honeycomb', detail: 'Traces and events', connected: false },
      { id: 'elastic', name: 'Elastic', detail: 'Logs and search over Elastic Cloud', connected: false },
      { id: 'splunk', name: 'Splunk', detail: 'Logs and Observability Cloud', connected: false },
      { id: 'sentry', name: 'Sentry', detail: 'Errors and releases', connected: false },
      { id: 'pagerduty', name: 'PagerDuty', detail: 'Incidents and on-call schedules', connected: false },
      { id: 'slack', name: 'Slack', detail: 'Channels and incident threads', connected: false },
      { id: 'cloudflare', name: 'Cloudflare', detail: 'DNS, tunnels and edge config', connected: false },
      { id: 'digitalocean', name: 'DigitalOcean', detail: 'Cloud resources and DOKS clusters', connected: false },
      { id: 'dockerhub', name: 'Docker Hub', detail: 'Images and tags', connected: false },
    ]);
    expect(watch.options).toEqual({ unshared: true });
  });

  it('folds the watch into the ChatGPT entry alone, the account as its detail once connected', () => {
    const live = connection({ phase: 'Connected', email: 'ada@example.com', registered: true });
    serve([connection({}), live]);

    const { result } = renderHook(() => useConnectors());

    expect(result.current.snapshotComplete).toBe(true);
    const chatgpt = result.current.connectors.find((c) => c.id === 'chatgpt')!;
    expect(chatgpt).toMatchObject({ connected: true, detail: 'ada@example.com' });
    expect(chatgpt.chatgpt?.connection).toBe(live);
    expect(result.current.connectors.filter((c) => c.chatgpt)).toHaveLength(1);
  });

  it("keeps the entry's own detail for a connection whose email is unknown", () => {
    serve([connection({ phase: 'Connected' })]);

    const { result } = renderHook(() => useConnectors());

    expect(result.current.connectors.find((c) => c.id === 'chatgpt')).toMatchObject({
      connected: true,
      detail: 'OpenAI models on your ChatGPT plan',
    });
  });

  it("keeps the entry's own detail while not connected", () => {
    serve([connection({ phase: 'Failed', failure: 'Declined' })]);

    const { result } = renderHook(() => useConnectors());

    const chatgpt = result.current.connectors.find((c) => c.id === 'chatgpt')!;
    expect(chatgpt).toMatchObject({ connected: false, detail: 'OpenAI models on your ChatGPT plan' });
  });

  it('runs each action as its mutation', () => {
    serve([connection({})]);
    const { result } = renderHook(() => useConnectors());
    const actions = result.current.connectors.find((c) => c.chatgpt)!.chatgpt!.actions;

    actions.connect(true);
    actions.cancel();
    actions.disconnect();
    actions.openUsage();

    expect(mutations.ChatGPTConnect).toHaveBeenCalledWith({ fresh: true });
    expect(mutations.ChatGPTConnectCancel).toHaveBeenCalledWith({});
    expect(mutations.ChatGPTDisconnect).toHaveBeenCalledWith({});
    expect(mutations.ChatGPTUsageOpen).toHaveBeenCalledWith({});
  });

  it('has not loaded until every watch has its first frame', () => {
    const { result, rerender } = renderHook(() => useConnectors());
    for (const missing of [
      'ChatGPTConnectionStateWatch',
      'OpenRouterConnectionStateWatch',
      'AWSConnectionStateWatch',
      'GitHubConnectionStateWatch',
      'GoogleConnectionStateWatch',
    ]) {
      serve([connection({})]);
      delete watch.frames[missing];
      rerender();
      expect(result.current.snapshotComplete, missing).toBe(false);
      expect(result.current.connectors.some((c) => c.chatgpt ?? c.openrouter ?? c.aws ?? c.github ?? c.google)).toBe(
        false,
      );
    }
  });

  it('folds the OpenRouter watch into its entry alone, connected in the Connected phase', () => {
    const live = openRouter({ phase: 'Connected' });
    serve([connection({})], [openRouter({ phase: 'SigningIn' }), live]);

    const { result } = renderHook(() => useConnectors());

    const entry = result.current.connectors.find((c) => c.id === 'openrouter')!;
    expect(entry).toMatchObject({ connected: true, detail: 'Many providers on one account' });
    expect(entry.openrouter?.connection).toBe(live);
    expect(result.current.connectors.filter((c) => c.openrouter)).toHaveLength(1);
  });

  it.each(['SignedOut', 'SigningIn', 'Failed'] as const)(
    'leaves the OpenRouter entry unconnected while %s',
    (phase) => {
      serve([connection({})], [openRouter({ phase })]);

      const { result } = renderHook(() => useConnectors());

      expect(result.current.connectors.find((c) => c.id === 'openrouter')).toMatchObject({ connected: false });
    },
  );

  it('runs each OpenRouter action as its mutation', () => {
    serve([connection({})]);
    const { result } = renderHook(() => useConnectors());
    const actions = result.current.connectors.find((c) => c.openrouter)!.openrouter!.actions;

    actions.connect();
    actions.cancel();
    actions.disconnect();
    actions.openKeys();

    expect(mutations.OpenRouterConnect).toHaveBeenCalledWith({});
    expect(mutations.OpenRouterConnectCancel).toHaveBeenCalledWith({});
    expect(mutations.OpenRouterDisconnect).toHaveBeenCalledWith({});
    expect(mutations.OpenRouterKeysOpen).toHaveBeenCalledWith({});
  });
});

describe('the AWS entry', () => {
  it('folds the AWS watch into its entry alone, the principal as its detail once connected', () => {
    const live = aws({ phase: 'Connected', arn: 'arn:aws:iam::123456789012:user/alice' });
    serve([connection({})], [openRouter()], [aws({ phase: 'SigningIn' }), live]);

    const { result } = renderHook(() => useConnectors());

    const entry = result.current.connectors.find((c) => c.id === 'aws')!;
    expect(entry).toMatchObject({ connected: true, detail: 'arn:aws:iam::123456789012:user/alice' });
    expect(entry.aws?.connection).toBe(live);
    expect(result.current.connectors.filter((c) => c.aws)).toHaveLength(1);
  });

  it("keeps the entry's own detail while not connected", () => {
    serve([connection({})], [openRouter()], [aws({ phase: 'Failed', failure: 'Declined' })]);

    const { result } = renderHook(() => useConnectors());

    expect(result.current.connectors.find((c) => c.id === 'aws')).toMatchObject({
      connected: false,
      detail: 'Cloud resources and Bedrock models',
    });
  });

  it('runs each AWS action as its mutation', () => {
    serve([connection({})]);
    const { result } = renderHook(() => useConnectors());
    const actions = result.current.connectors.find((c) => c.aws)!.aws!.actions;

    actions.connect();
    actions.cancel();
    actions.disconnect();

    expect(mutations.AWSConnect).toHaveBeenCalledWith({});
    expect(mutations.AWSConnectCancel).toHaveBeenCalledWith({});
    expect(mutations.AWSDisconnect).toHaveBeenCalledWith({});
  });
});

describe('useConnectors: GitHub', () => {
  it('folds the GitHub watch into its entry alone, the login as its detail once connected', () => {
    const live = gitHub({ phase: 'Connected', login: 'octocat' });
    serve([connection({})], undefined, undefined, [gitHub({ phase: 'SigningIn', userCode: 'WDJB-MJHT' }), live]);

    const { result } = renderHook(() => useConnectors());

    const entry = result.current.connectors.find((c) => c.id === 'github')!;
    expect(entry).toMatchObject({ connected: true, detail: 'octocat' });
    expect(entry.github?.connection).toBe(live);
    expect(result.current.connectors.filter((c) => c.github)).toHaveLength(1);
  });

  it.each(['SignedOut', 'SigningIn', 'Failed'] as const)("keeps the entry's own detail while %s", (phase) => {
    serve([connection({})], undefined, undefined, [gitHub({ phase, login: 'octocat' })]);

    const { result } = renderHook(() => useConnectors());

    expect(result.current.connectors.find((c) => c.id === 'github')).toMatchObject({
      connected: false,
      detail: 'Repositories for git and gh in the sandbox',
    });
  });

  it('runs each GitHub action as its mutation', () => {
    serve([connection({})]);
    const { result } = renderHook(() => useConnectors());
    const actions = result.current.connectors.find((c) => c.github)!.github!.actions;

    actions.connect();
    actions.cancel();
    actions.disconnect();
    actions.openSettings();

    expect(mutations.GitHubConnect).toHaveBeenCalledWith({});
    expect(mutations.GitHubConnectCancel).toHaveBeenCalledWith({});
    expect(mutations.GitHubDisconnect).toHaveBeenCalledWith({});
    expect(mutations.GitHubSettingsOpen).toHaveBeenCalledWith({});
  });
});

describe('useConnectors: Google', () => {
  it('folds the Google watch into its entry alone, the account as its detail once connected', () => {
    const live = google({ phase: 'Connected', email: 'ada@example.com', project: 'my-project' });
    serve([connection({})], undefined, undefined, undefined, [google({ phase: 'SigningIn' }), live]);

    const { result } = renderHook(() => useConnectors());

    const entry = result.current.connectors.find((c) => c.id === 'google')!;
    expect(entry).toMatchObject({ connected: true, detail: 'ada@example.com' });
    expect(entry.google?.connection).toBe(live);
    expect(result.current.connectors.filter((c) => c.google)).toHaveLength(1);
  });

  it("keeps the entry's own detail while connected with no email", () => {
    serve([connection({})], undefined, undefined, undefined, [google({ phase: 'Connected' })]);

    const { result } = renderHook(() => useConnectors());

    expect(result.current.connectors.find((c) => c.id === 'google')).toMatchObject({
      connected: true,
      detail: 'Gemini models on Vertex AI',
    });
  });

  it('runs each Google action as its mutation', () => {
    serve([connection({})]);
    const { result } = renderHook(() => useConnectors());
    const actions = result.current.connectors.find((c) => c.google)!.google!.actions;

    actions.connect(true);
    actions.cancel();
    actions.disconnect();

    expect(mutations.GoogleConnect).toHaveBeenCalledWith({ fresh: true });
    expect(mutations.GoogleConnectCancel).toHaveBeenCalledWith({});
    expect(mutations.GoogleDisconnect).toHaveBeenCalledWith({});
  });
});

describe('useOpenChatGPTUsage', () => {
  it("asks the sidecar to open ChatGPT's usage settings", () => {
    const { result } = renderHook(() => useOpenChatGPTUsage());

    result.current();

    expect(mutations.ChatGPTUsageOpen).toHaveBeenCalledWith({});
  });
});

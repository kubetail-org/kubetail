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

// The services Kubetail signs in to, whose credentials providers and tools use.
// ChatGPT, OpenRouter, AWS, GitHub and Google are real: each entry folds its
// connection's watch, and the sidecar runs the sign-in and opens the browser
// itself. The rest are placeholders.
import { useCallback, useMemo } from 'react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import type {
  AwsConnectionStateWatchSubscription,
  ChatGptConnectionStateWatchSubscription,
  GitHubConnectionStateWatchSubscription,
  GoogleConnectionStateWatchSubscription,
  OpenRouterConnectionStateWatchSubscription,
} from '@/gql/graphql';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

/** Why a connector's last attempt, session or key ended: one enum for every connector. */
export type { ConnectorFailure } from '@/gql/graphql';

/** The ChatGPT plan's provider id, and its connector's: the sidecar's `catalog.ChatGPTID`. */
export const CHATGPT_ID = 'chatgpt';

export type ChatGPTConnection = ChatGptConnectionStateWatchSubscription['chatGPTConnectionStateWatch'];

/** What a connected or failed ChatGPT entry can do. Each outcome arrives on the watch. */
export type ChatGPTActions = {
  connect: (fresh: boolean) => void;
  cancel: () => void;
  disconnect: () => void;
  openUsage: () => void;
};

/** The OpenRouter connector's id, and its provider's: the sidecar's `openrouter` row. */
export const OPENROUTER_ID = 'openrouter';

export type OpenRouterConnection = OpenRouterConnectionStateWatchSubscription['openRouterConnectionStateWatch'];

/** What the OpenRouter entry can do. Each outcome arrives on the watch. */
export type OpenRouterActions = {
  connect: () => void;
  cancel: () => void;
  disconnect: () => void;
  openKeys: () => void;
};

/** The AWS connector's id. */
export const AWS_ID = 'aws';

export type AWSConnection = AwsConnectionStateWatchSubscription['awsConnectionStateWatch'];

/** What the AWS entry can do. Each outcome arrives on the watch. */
export type AWSActions = {
  connect: () => void;
  cancel: () => void;
  disconnect: () => void;
};

/** The GitHub connector's id: the sidecar's `github.ID`. */
export const GITHUB_ID = 'github';

export type GitHubConnection = GitHubConnectionStateWatchSubscription['gitHubConnectionStateWatch'];

/** What the GitHub entry can do. Each outcome arrives on the watch. */
export type GitHubActions = {
  connect: () => void;
  cancel: () => void;
  disconnect: () => void;
  openSettings: () => void;
};

/** The Google connector's id, and its provider's: the sidecar's `google` row. */
export const GOOGLE_ID = 'google';

export type GoogleConnection = GoogleConnectionStateWatchSubscription['googleConnectionStateWatch'];

/** What the Google entry can do. Each outcome arrives on the watch. */
export type GoogleActions = {
  connect: (fresh: boolean) => void;
  cancel: () => void;
  disconnect: () => void;
};

export type Connector = {
  id: string;
  name: string;
  // The second line: the account, profile or what the service offers.
  detail: string;
  connected: boolean;
  // The label of the item that opens the service's own settings, if it has one.
  manageLabel?: string;
  // A real connector's own: its connection and actions. Absent on every other entry.
  chatgpt?: { connection: ChatGPTConnection; actions: ChatGPTActions };
  openrouter?: { connection: OpenRouterConnection; actions: OpenRouterActions };
  aws?: { connection: AWSConnection; actions: AWSActions };
  github?: { connection: GitHubConnection; actions: GitHubActions };
  google?: { connection: GoogleConnection; actions: GoogleActions };
};

export type Connectors = {
  connectors: Connector[];
  // Every watch's first frame has landed; nothing draws before them.
  snapshotComplete: boolean;
};

// The connectors Kubetail plans to ship; the tab sorts them by name. Each but ChatGPT,
// OpenRouter, AWS, GitHub and Google is a placeholder until its backend exists.
const PLANNED: Connector[] = [
  { id: GITHUB_ID, name: 'GitHub', detail: 'Repositories for git and gh in the sandbox', connected: false },
  { id: AWS_ID, name: 'AWS', detail: 'Cloud resources and Bedrock models', connected: false },
  { id: GOOGLE_ID, name: 'Google', detail: 'Gemini models on Vertex AI', connected: false },
  { id: 'azure', name: 'Azure', detail: 'Cloud resources and Foundry models', connected: false },
  { id: CHATGPT_ID, name: 'ChatGPT', detail: 'OpenAI models on your ChatGPT plan', connected: false },
  { id: OPENROUTER_ID, name: 'OpenRouter', detail: 'Many providers on one account', connected: false },
  { id: 'gitlab', name: 'GitLab', detail: 'Code, pipelines and the container registry', connected: false },
  { id: 'atlassian', name: 'Atlassian', detail: 'Jira issues, Confluence pages and Opsgenie alerts', connected: false },
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
];

const ChatGPTConnectionStateWatchDocument = graphql(`
  subscription ChatGPTConnectionStateWatch {
    chatGPTConnectionStateWatch {
      phase
      email
      failure
      registered
      revocationUnconfirmed
    }
  }
`);

const ChatGPTConnectDocument = graphql(`
  mutation ChatGPTConnect($fresh: Boolean!) {
    chatGPTConnect(fresh: $fresh)
  }
`);

const ChatGPTConnectCancelDocument = graphql(`
  mutation ChatGPTConnectCancel {
    chatGPTConnectCancel
  }
`);

const ChatGPTDisconnectDocument = graphql(`
  mutation ChatGPTDisconnect {
    chatGPTDisconnect
  }
`);

const ChatGPTUsageOpenDocument = graphql(`
  mutation ChatGPTUsageOpen {
    chatGPTUsageOpen
  }
`);

const OpenRouterConnectionStateWatchDocument = graphql(`
  subscription OpenRouterConnectionStateWatch {
    openRouterConnectionStateWatch {
      phase
      failure
    }
  }
`);

const OpenRouterConnectDocument = graphql(`
  mutation OpenRouterConnect {
    openRouterConnect
  }
`);

const OpenRouterConnectCancelDocument = graphql(`
  mutation OpenRouterConnectCancel {
    openRouterConnectCancel
  }
`);

const OpenRouterDisconnectDocument = graphql(`
  mutation OpenRouterDisconnect {
    openRouterDisconnect
  }
`);

const OpenRouterKeysOpenDocument = graphql(`
  mutation OpenRouterKeysOpen {
    openRouterKeysOpen
  }
`);

const AWSConnectionStateWatchDocument = graphql(`
  subscription AWSConnectionStateWatch {
    awsConnectionStateWatch {
      phase
      arn
      failure
    }
  }
`);

const AWSConnectDocument = graphql(`
  mutation AWSConnect {
    awsConnect
  }
`);

const AWSConnectCancelDocument = graphql(`
  mutation AWSConnectCancel {
    awsConnectCancel
  }
`);

const AWSDisconnectDocument = graphql(`
  mutation AWSDisconnect {
    awsDisconnect
  }
`);

const GitHubConnectionStateWatchDocument = graphql(`
  subscription GitHubConnectionStateWatch {
    gitHubConnectionStateWatch {
      phase
      login
      userCode
      failure
    }
  }
`);

const GitHubConnectDocument = graphql(`
  mutation GitHubConnect {
    gitHubConnect
  }
`);

const GitHubConnectCancelDocument = graphql(`
  mutation GitHubConnectCancel {
    gitHubConnectCancel
  }
`);

const GitHubDisconnectDocument = graphql(`
  mutation GitHubDisconnect {
    gitHubDisconnect
  }
`);

const GitHubSettingsOpenDocument = graphql(`
  mutation GitHubSettingsOpen {
    gitHubSettingsOpen
  }
`);

const GoogleConnectionStateWatchDocument = graphql(`
  subscription GoogleConnectionStateWatch {
    googleConnectionStateWatch {
      phase
      email
      project
      failure
      revocationUnconfirmed
    }
  }
`);

const GoogleConnectDocument = graphql(`
  mutation GoogleConnect($fresh: Boolean!) {
    googleConnect(fresh: $fresh)
  }
`);

const GoogleConnectCancelDocument = graphql(`
  mutation GoogleConnectCancel {
    googleConnectCancel
  }
`);

const GoogleDisconnectDocument = graphql(`
  mutation GoogleDisconnect {
    googleDisconnect
  }
`);

/** Opens ChatGPT's usage settings in the browser, at the sidecar's own address. */
export function useOpenChatGPTUsage(): () => void {
  const [, run] = useMutation(ChatGPTUsageOpenDocument);
  return useCallback(() => void run({}), [run]);
}

export function useConnectors(): Connectors {
  // Unshared: a state watch's first frame is its snapshot, and the tab remounts each time
  // it is shown.
  const { data: chatGPT } = useFoldedSubscription<ChatGptConnectionStateWatchSubscription, ChatGPTConnection>(
    { query: ChatGPTConnectionStateWatchDocument },
    stateWatchReducer((next) => next.chatGPTConnectionStateWatch),
    { unshared: true },
  );
  const { data: openRouter } = useFoldedSubscription<OpenRouterConnectionStateWatchSubscription, OpenRouterConnection>(
    { query: OpenRouterConnectionStateWatchDocument },
    stateWatchReducer((next) => next.openRouterConnectionStateWatch),
    { unshared: true },
  );
  const { data: aws } = useFoldedSubscription<AwsConnectionStateWatchSubscription, AWSConnection>(
    { query: AWSConnectionStateWatchDocument },
    stateWatchReducer((next) => next.awsConnectionStateWatch),
    { unshared: true },
  );
  const { data: gitHub } = useFoldedSubscription<GitHubConnectionStateWatchSubscription, GitHubConnection>(
    { query: GitHubConnectionStateWatchDocument },
    stateWatchReducer((next) => next.gitHubConnectionStateWatch),
    { unshared: true },
  );
  const { data: google } = useFoldedSubscription<GoogleConnectionStateWatchSubscription, GoogleConnection>(
    { query: GoogleConnectionStateWatchDocument },
    stateWatchReducer((next) => next.googleConnectionStateWatch),
    { unshared: true },
  );
  const chatGPTActions = useChatGPTActions();
  const openRouterActions = useOpenRouterActions();
  const awsActions = useAWSActions();
  const gitHubActions = useGitHubActions();
  const googleActions = useGoogleActions();
  return useMemo(() => {
    if (!chatGPT || !openRouter || !aws || !gitHub || !google) return { connectors: PLANNED, snapshotComplete: false };
    const connectors = PLANNED.map((c): Connector => {
      switch (c.id) {
        case CHATGPT_ID: {
          const connected = chatGPT.phase === 'Connected';
          return {
            ...c,
            connected,
            detail: (connected && chatGPT.email) || c.detail,
            chatgpt: { connection: chatGPT, actions: chatGPTActions },
          };
        }
        case OPENROUTER_ID:
          return {
            ...c,
            connected: openRouter.phase === 'Connected',
            openrouter: { connection: openRouter, actions: openRouterActions },
          };
        case AWS_ID: {
          const connected = aws.phase === 'Connected';
          return {
            ...c,
            connected,
            detail: (connected && aws.arn) || c.detail,
            aws: { connection: aws, actions: awsActions },
          };
        }
        case GITHUB_ID: {
          const connected = gitHub.phase === 'Connected';
          return {
            ...c,
            connected,
            detail: (connected && gitHub.login) || c.detail,
            github: { connection: gitHub, actions: gitHubActions },
          };
        }
        case GOOGLE_ID: {
          const connected = google.phase === 'Connected';
          return {
            ...c,
            connected,
            detail: (connected && google.email) || c.detail,
            google: { connection: google, actions: googleActions },
          };
        }
        default:
          return c;
      }
    });
    return { connectors, snapshotComplete: true };
  }, [
    chatGPT,
    chatGPTActions,
    openRouter,
    openRouterActions,
    aws,
    awsActions,
    gitHub,
    gitHubActions,
    google,
    googleActions,
  ]);
}

// A mutation's error reaches the error bus; its outcome is the watch's.
function useChatGPTActions(): ChatGPTActions {
  const [, connect] = useMutation(ChatGPTConnectDocument);
  const [, cancel] = useMutation(ChatGPTConnectCancelDocument);
  const [, disconnect] = useMutation(ChatGPTDisconnectDocument);
  const openUsage = useOpenChatGPTUsage();
  return useMemo(
    () => ({
      connect: (fresh) => void connect({ fresh }),
      cancel: () => void cancel({}),
      disconnect: () => void disconnect({}),
      openUsage,
    }),
    [connect, cancel, disconnect, openUsage],
  );
}

function useOpenRouterActions(): OpenRouterActions {
  const [, connect] = useMutation(OpenRouterConnectDocument);
  const [, cancel] = useMutation(OpenRouterConnectCancelDocument);
  const [, disconnect] = useMutation(OpenRouterDisconnectDocument);
  const [, openKeys] = useMutation(OpenRouterKeysOpenDocument);
  return useMemo(
    () => ({
      connect: () => void connect({}),
      cancel: () => void cancel({}),
      disconnect: () => void disconnect({}),
      openKeys: () => void openKeys({}),
    }),
    [connect, cancel, disconnect, openKeys],
  );
}

function useAWSActions(): AWSActions {
  const [, connect] = useMutation(AWSConnectDocument);
  const [, cancel] = useMutation(AWSConnectCancelDocument);
  const [, disconnect] = useMutation(AWSDisconnectDocument);
  return useMemo(
    () => ({
      connect: () => void connect({}),
      cancel: () => void cancel({}),
      disconnect: () => void disconnect({}),
    }),
    [connect, cancel, disconnect],
  );
}

function useGitHubActions(): GitHubActions {
  const [, connect] = useMutation(GitHubConnectDocument);
  const [, cancel] = useMutation(GitHubConnectCancelDocument);
  const [, disconnect] = useMutation(GitHubDisconnectDocument);
  const [, openSettings] = useMutation(GitHubSettingsOpenDocument);
  return useMemo(
    () => ({
      connect: () => void connect({}),
      cancel: () => void cancel({}),
      disconnect: () => void disconnect({}),
      openSettings: () => void openSettings({}),
    }),
    [connect, cancel, disconnect, openSettings],
  );
}

function useGoogleActions(): GoogleActions {
  const [, connect] = useMutation(GoogleConnectDocument);
  const [, cancel] = useMutation(GoogleConnectCancelDocument);
  const [, disconnect] = useMutation(GoogleDisconnectDocument);
  return useMemo(
    () => ({
      connect: (fresh: boolean) => void connect({ fresh }),
      cancel: () => void cancel({}),
      disconnect: () => void disconnect({}),
    }),
    [connect, cancel, disconnect],
  );
}

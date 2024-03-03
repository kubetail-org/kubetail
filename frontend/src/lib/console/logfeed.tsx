// Copyright 2024 Andres Morey
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

import { useEffect } from 'react';
import { RecoilRoot, atom, useRecoilState, useRecoilValue } from 'recoil';

import type { LogRecord as GraphQLLogRecord } from '@/lib/graphql/__generated__/graphql';

import { Node, Pod } from './types';

/**
 * Types
 */

export enum LogFeedState {
  Streaming = 'STREAMING',
  Paused = 'PAUSED',
  InQuery = 'IN_QUERY',
}

interface LogRecord extends GraphQLLogRecord {
  node: Node;
  pod: Pod;
  container: string;
};

/**
 * State
 */

const isReadyState = atom({
  key: 'isReady',
  default: false,
});

const isLoadingState = atom({
  key: 'isLoading',
  default: true,
});

const feedStateState = atom({
  key: 'feedState',
  default: LogFeedState.Streaming,
});

const recordsState = atom({
  key: 'records',
  default: new Array<LogRecord>(),
});

/**
 * Hooks
 */

export const useLogFeedControls = () => {
  const [, setFeedState] = useRecoilState(feedStateState);

  return {
    startStreaming: () => {
      setFeedState(LogFeedState.Streaming);
    },
    stopStreaming: () => {
      setFeedState(LogFeedState.Paused);
    },
    skipForward: () => {
      console.log('skip-forward');
    },
  };
};

export const useLogFeedMetadata = () => {
  const isReady = useRecoilValue(isReadyState);
  const isLoading = useRecoilValue(isLoadingState);
  const state = useRecoilValue(feedStateState);
  return { isReady, isLoading, state };
};

/**
 * LogFeedViewer component
 */

export const LogFeedViewer = () => {
  return <div>hi</div>;
};

/**
 * LogFeedProvider component
 */

interface LogFeedProvider extends React.PropsWithChildren {
  defaultSince?: string;
  defaultUntil?: string;
}

export const LogFeedProvider = ({ defaultSince, defaultUntil, children}: LogFeedProvider) => {
  useEffect(() => {
    console.log(`${defaultSince}_${defaultUntil}`);
  }, [defaultSince, defaultUntil]);

  return (
    <RecoilRoot>
      {children}
    </RecoilRoot>
  );
};

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

import { useQuery } from '@apollo/client';
import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react';
import { RecoilRoot, atom, useRecoilState, useRecoilValue } from 'recoil';

import type { LogRecord as GraphQLLogRecord } from '@/lib/graphql/__generated__/graphql';
import * as ops from '@/lib/graphql/ops';

import { useNodes, usePods } from './hooks';
import { Node, Pod } from './types';

/**
 * Types
 */

type LogFeedQueryOptions = {
  since?: string;
  until?: string;
};

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
 * LogFeedRecordFetcher component
 */

type LogFeedRecordFetcherProps = {
  node: Node;
  pod: Pod;
  container: string;
  onLoad?: (records: LogRecord[]) => void;
  onUpdate?: (record: LogRecord) => void;
};

type LogFeedRecordFetcherHandle = {
  skipForward: () => Promise<LogRecord[]>;
  query: (opts: LogFeedQueryOptions) => Promise<LogRecord[]>;
};

const LogFeedDataFetcherImpl: React.ForwardRefRenderFunction<LogFeedRecordFetcherHandle, LogFeedRecordFetcherProps> = (props, ref) => {
  const { node, pod, container, onLoad, onUpdate } = props;
  const { namespace, name } = pod.metadata;
  const feedState = useRecoilValue(feedStateState);
  const [, setRecords] = useRecoilState(recordsState);

  const lastTSRef = useRef<string>();
  const startTSRef = useRef<string>();

  const upgradeRecord = (record: GraphQLLogRecord) => {
    return { ...record, node, pod, container };
  };

  // get logs
  const { loading, data, subscribeToMore, refetch } = useQuery(ops.QUERY_CONTAINER_LOG, {
    variables: { namespace, name, container },
    fetchPolicy: 'no-cache',
    skip: true,  // we'll use refetch() and subscribeToMmore() instead
    onCompleted: (data) => {
      if (!data?.podLogQuery) return;
      // execute callback
      onLoad && onLoad(data.podLogQuery.map(record => upgradeRecord(record)));
    },
    onError: (err) => {
      console.log(err);
    },
  });

  // update lastTS
  if (!lastTSRef.current) lastTSRef.current = data?.podLogQuery?.length ? data.podLogQuery[data.podLogQuery.length - 1].timestamp : undefined;

  // tail
  useEffect(() => {
    // wait for initial query to complete
    if (!(loading === false)) return;

    // only execute when playing
    if (!(feedState === LogFeedState.Streaming)) return;

    // update startTS
    startTSRef.current = (new Date()).toISOString();

    const variables = { namespace, name, container } as any;

    // implement `after`
    if (lastTSRef.current) variables.after = lastTSRef.current;
    else variables.since = 'NOW';

    return subscribeToMore({
      document: ops.TAIL_CONTAINER_LOG,
      variables: variables,
      updateQuery: (_, { subscriptionData }) => {
        const record = subscriptionData.data.podLogTail;
        if (record) {
          // update lastTS
          lastTSRef.current = record.timestamp;

          // update records
          setRecords(oldRecords => [...oldRecords, upgradeRecord(record)]);
        }
        return { podLogQuery: [] };
      },
      onError: (err) => {
        console.log(err)
      },
    });
  }, [subscribeToMore, loading, feedState]);

  // define handler api
  useImperativeHandle(ref, () => ({
    skipForward: async () => {
      const variables = {} as any;
      if (lastTSRef.current) variables.after = lastTSRef.current;
      else variables.after = startTSRef.current;
      
      const result = await refetch(variables);
      if (!result.data.podLogQuery) return [];

      // upgrade records
      const records = result.data.podLogQuery.map(record => upgradeRecord(record));

      // update lastTS
      if (records.length) lastTSRef.current = records[records.length - 1].timestamp;

      // return records
      return records;
    },
    query: async (opts: LogFeedQueryOptions) => {
      const result = await refetch(opts);
      if (!result.data.podLogQuery) return [];

      // upgrade records
      const records = result.data.podLogQuery.map(record => upgradeRecord(record));

      // update lastTS
      if (!opts.until) {
        if (records.length) lastTSRef.current = records[records.length - 1].timestamp;
        else lastTSRef.current = undefined;
      }

      // return records
      return records;
    }
  }));

  return <></>;
};

const LogFeedDataFetcher = forwardRef(LogFeedDataFetcherImpl);

/**
 * LogFeedLoader component
 */

const LogFeedLoader = () => {
  const nodes = useNodes();
  const pods = usePods();
  const [, setIsReadyState] = useRecoilState(isReadyState);

  // set isReady after component and children are mounted
  useEffect(() => {
    if (nodes.loading || pods.loading) return;
    setIsReadyState(true);
  }, [nodes.loading, pods.loading]);

  // only load containers from nodes that we have a record of
  const nodeMap = new Map(nodes.nodes.map(node => [node.metadata.name, node]));

  const els: JSX.Element[] = [];
  pods.pods.forEach(pod => {
    pod.status.containerStatuses.forEach(status => {
      const node = nodeMap.get(pod.spec.nodeName);
      if (status.started && node) {
        els.push(
          <LogFeedDataFetcher
            key={`${pod.metadata.namespace}/${pod.metadata.name}/${status.name}`}
            node={node}
            pod={pod}
            container={status.name}
          />
        );
      }
    });
  });

  return <>{els}</>;
};

/**
 * LogFeedProvider component
 */

interface LogFeedProvider extends React.PropsWithChildren {
  defaultSince?: string;
  defaultUntil?: string;
}

export const LogFeedProvider = ({ defaultSince, defaultUntil, children}: LogFeedProvider) => {
  return (
    <RecoilRoot>
      <LogFeedLoader />
      {children}
    </RecoilRoot>
  );
};

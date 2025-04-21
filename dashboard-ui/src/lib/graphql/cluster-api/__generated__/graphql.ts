/* eslint-disable */
import { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';
export type Maybe<T> = T | null;
export type InputMaybe<T> = Maybe<T>;
export type Exact<T extends { [key: string]: unknown }> = { [K in keyof T]: T[K] };
export type MakeOptional<T, K extends keyof T> = Omit<T, K> & { [SubKey in K]?: Maybe<T[SubKey]> };
export type MakeMaybe<T, K extends keyof T> = Omit<T, K> & { [SubKey in K]: Maybe<T[SubKey]> };
export type MakeEmpty<T extends { [key: string]: unknown }, K extends keyof T> = { [_ in K]?: never };
export type Incremental<T> = T | { [P in keyof T]?: P extends ' $fragmentName' | '__typename' ? T[P] : never };
/** All built-in and custom scalars, mapped to their actual values */
export type Scalars = {
  ID: { input: string; output: string; }
  String: { input: string; output: string; }
  Boolean: { input: boolean; output: boolean; }
  Int: { input: number; output: number; }
  Float: { input: number; output: number; }
  /** A 64-bit integer. */
  Int64: { input: any; output: any; }
  /** An ISO-8601 encoded UTC date string. */
  Time: { input: any; output: any; }
  /** An ISO-8601 encoded UTC date string. */
  TimestampPBTimestamp: { input: any; output: any; }
};

export type LogMetadata = {
  __typename?: 'LogMetadata';
  fileInfo: LogMetadataFileInfo;
  id: Scalars['ID']['output'];
  spec: LogMetadataSpec;
};

export type LogMetadataFileInfo = {
  __typename?: 'LogMetadataFileInfo';
  lastModifiedAt?: Maybe<Scalars['TimestampPBTimestamp']['output']>;
  size: Scalars['Int64']['output'];
};

export type LogMetadataList = {
  __typename?: 'LogMetadataList';
  items: Array<LogMetadata>;
};

export type LogMetadataSpec = {
  __typename?: 'LogMetadataSpec';
  containerID: Scalars['ID']['output'];
  containerName: Scalars['String']['output'];
  namespace: Scalars['String']['output'];
  nodeName: Scalars['String']['output'];
  podName: Scalars['String']['output'];
};

export type LogMetadataWatchEvent = {
  __typename?: 'LogMetadataWatchEvent';
  object?: Maybe<LogMetadata>;
  type: Scalars['String']['output'];
};

export type LogRecord = {
  __typename?: 'LogRecord';
  message: Scalars['String']['output'];
  timestamp: Scalars['Time']['output'];
};

export type PageInfo = {
  __typename?: 'PageInfo';
  /** When paginating forwards, the cursor to continue. */
  endCursor?: Maybe<Scalars['ID']['output']>;
  /** When paginating forwards, are there more items? */
  hasNextPage: Scalars['Boolean']['output'];
  /** When paginating backwards, are there more items? */
  hasPreviousPage: Scalars['Boolean']['output'];
  /** When paginating backwards, the cursor to continue. */
  startCursor?: Maybe<Scalars['ID']['output']>;
};

export type PodLogQueryResponse = {
  __typename?: 'PodLogQueryResponse';
  pageInfo: PageInfo;
  results: Array<LogRecord>;
};

export type Query = {
  __typename?: 'Query';
  /** Log Metadata API */
  logMetadataList?: Maybe<LogMetadataList>;
  /** Pod logs API */
  podLogHead?: Maybe<PodLogQueryResponse>;
  podLogTail?: Maybe<PodLogQueryResponse>;
};


export type QueryLogMetadataListArgs = {
  namespace?: InputMaybe<Scalars['String']['input']>;
};


export type QueryPodLogHeadArgs = {
  after?: InputMaybe<Scalars['ID']['input']>;
  container?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  kubeContext?: InputMaybe<Scalars['String']['input']>;
  name: Scalars['String']['input'];
  namespace?: InputMaybe<Scalars['String']['input']>;
  node?: InputMaybe<Scalars['String']['input']>;
  query?: InputMaybe<Scalars['String']['input']>;
  since?: InputMaybe<Scalars['String']['input']>;
};


export type QueryPodLogTailArgs = {
  before?: InputMaybe<Scalars['ID']['input']>;
  container?: InputMaybe<Scalars['String']['input']>;
  kubeContext?: InputMaybe<Scalars['String']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  name: Scalars['String']['input'];
  namespace?: InputMaybe<Scalars['String']['input']>;
  node?: InputMaybe<Scalars['String']['input']>;
  query?: InputMaybe<Scalars['String']['input']>;
};

export type Subscription = {
  __typename?: 'Subscription';
  /** Log Metadata API */
  logMetadataWatch?: Maybe<LogMetadataWatchEvent>;
  /** Pod logs API */
  podLogFollow?: Maybe<LogRecord>;
};


export type SubscriptionLogMetadataWatchArgs = {
  namespace?: InputMaybe<Scalars['String']['input']>;
};


export type SubscriptionPodLogFollowArgs = {
  after?: InputMaybe<Scalars['ID']['input']>;
  container?: InputMaybe<Scalars['String']['input']>;
  kubeContext?: InputMaybe<Scalars['String']['input']>;
  name: Scalars['String']['input'];
  namespace?: InputMaybe<Scalars['String']['input']>;
  node?: InputMaybe<Scalars['String']['input']>;
  query?: InputMaybe<Scalars['String']['input']>;
  since?: InputMaybe<Scalars['String']['input']>;
};

export type LogMetadataListItemFragmentFragment = { __typename?: 'LogMetadata', id: string, spec: { __typename?: 'LogMetadataSpec', nodeName: string, namespace: string, podName: string, containerName: string, containerID: string }, fileInfo: { __typename?: 'LogMetadataFileInfo', size: any, lastModifiedAt?: any | null } };

export type LogMetadataListFetchQueryVariables = Exact<{
  namespace?: InputMaybe<Scalars['String']['input']>;
}>;


export type LogMetadataListFetchQuery = { __typename?: 'Query', logMetadataList?: { __typename?: 'LogMetadataList', items: Array<{ __typename?: 'LogMetadata', id: string, spec: { __typename?: 'LogMetadataSpec', nodeName: string, namespace: string, podName: string, containerName: string, containerID: string }, fileInfo: { __typename?: 'LogMetadataFileInfo', size: any, lastModifiedAt?: any | null } }> } | null };

export type LogMetadataListWatchSubscriptionVariables = Exact<{
  namespace?: InputMaybe<Scalars['String']['input']>;
}>;


export type LogMetadataListWatchSubscription = { __typename?: 'Subscription', logMetadataWatch?: { __typename?: 'LogMetadataWatchEvent', type: string, object?: { __typename?: 'LogMetadata', id: string, spec: { __typename?: 'LogMetadataSpec', nodeName: string, namespace: string, podName: string, containerName: string, containerID: string }, fileInfo: { __typename?: 'LogMetadataFileInfo', size: any, lastModifiedAt?: any | null } } | null } | null };

export const LogMetadataListItemFragmentFragmentDoc = {"kind":"Document","definitions":[{"kind":"FragmentDefinition","name":{"kind":"Name","value":"LogMetadataListItemFragment"},"typeCondition":{"kind":"NamedType","name":{"kind":"Name","value":"LogMetadata"}},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nodeName"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"podName"}},{"kind":"Field","name":{"kind":"Name","value":"containerName"}},{"kind":"Field","name":{"kind":"Name","value":"containerID"}}]}},{"kind":"Field","name":{"kind":"Name","value":"fileInfo"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"size"}},{"kind":"Field","name":{"kind":"Name","value":"lastModifiedAt"}}]}}]}}]} as unknown as DocumentNode<LogMetadataListItemFragmentFragment, unknown>;
export const LogMetadataListFetchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"LogMetadataListFetch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"namespace"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}},"defaultValue":{"kind":"StringValue","value":"","block":false}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"logMetadataList"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"namespace"},"value":{"kind":"Variable","name":{"kind":"Name","value":"namespace"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"items"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"FragmentSpread","name":{"kind":"Name","value":"LogMetadataListItemFragment"}}]}}]}}]}},{"kind":"FragmentDefinition","name":{"kind":"Name","value":"LogMetadataListItemFragment"},"typeCondition":{"kind":"NamedType","name":{"kind":"Name","value":"LogMetadata"}},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nodeName"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"podName"}},{"kind":"Field","name":{"kind":"Name","value":"containerName"}},{"kind":"Field","name":{"kind":"Name","value":"containerID"}}]}},{"kind":"Field","name":{"kind":"Name","value":"fileInfo"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"size"}},{"kind":"Field","name":{"kind":"Name","value":"lastModifiedAt"}}]}}]}}]} as unknown as DocumentNode<LogMetadataListFetchQuery, LogMetadataListFetchQueryVariables>;
export const LogMetadataListWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"LogMetadataListWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"namespace"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}},"defaultValue":{"kind":"StringValue","value":"","block":false}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"logMetadataWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"namespace"},"value":{"kind":"Variable","name":{"kind":"Name","value":"namespace"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"object"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"FragmentSpread","name":{"kind":"Name","value":"LogMetadataListItemFragment"}}]}}]}}]}},{"kind":"FragmentDefinition","name":{"kind":"Name","value":"LogMetadataListItemFragment"},"typeCondition":{"kind":"NamedType","name":{"kind":"Name","value":"LogMetadata"}},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nodeName"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"podName"}},{"kind":"Field","name":{"kind":"Name","value":"containerName"}},{"kind":"Field","name":{"kind":"Name","value":"containerID"}}]}},{"kind":"Field","name":{"kind":"Name","value":"fileInfo"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"size"}},{"kind":"Field","name":{"kind":"Name","value":"lastModifiedAt"}}]}}]}}]} as unknown as DocumentNode<LogMetadataListWatchSubscription, LogMetadataListWatchSubscriptionVariables>;
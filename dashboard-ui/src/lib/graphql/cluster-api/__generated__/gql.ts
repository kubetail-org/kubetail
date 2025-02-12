/* eslint-disable */
import * as types from './graphql';
import { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';

/**
 * Map of all GraphQL operations in the project.
 *
 * This map has several performance disadvantages:
 * 1. It is not tree-shakeable, so it will include all operations in the project.
 * 2. It is not minifiable, so the string of a GraphQL query will be multiple times inside the bundle.
 * 3. It does not support dead code elimination, so it will add unused operations.
 *
 * Therefore it is highly recommended to use the babel or swc plugin for production.
 * Learn more about it here: https://the-guild.dev/graphql/codegen/plugins/presets/preset-client#reducing-bundle-size
 */
type Documents = {
    "\n  fragment LogMetadataListItemFragment on LogMetadata {\n    id\n    spec {\n      nodeName\n      namespace\n      podName\n      containerName\n      containerID\n    }\n    fileInfo {\n      size\n      lastModifiedAt\n    }\n  }\n": typeof types.LogMetadataListItemFragmentFragmentDoc,
    "\n  query LogMetadataListFetch($namespace: String = \"\") {\n    logMetadataList(namespace: $namespace) {\n      items {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n": typeof types.LogMetadataListFetchDocument,
    "\n  subscription LogMetadataListWatch($namespace: String = \"\") {\n    logMetadataWatch(namespace: $namespace) {\n      type\n      object {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n": typeof types.LogMetadataListWatchDocument,
};
const documents: Documents = {
    "\n  fragment LogMetadataListItemFragment on LogMetadata {\n    id\n    spec {\n      nodeName\n      namespace\n      podName\n      containerName\n      containerID\n    }\n    fileInfo {\n      size\n      lastModifiedAt\n    }\n  }\n": types.LogMetadataListItemFragmentFragmentDoc,
    "\n  query LogMetadataListFetch($namespace: String = \"\") {\n    logMetadataList(namespace: $namespace) {\n      items {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n": types.LogMetadataListFetchDocument,
    "\n  subscription LogMetadataListWatch($namespace: String = \"\") {\n    logMetadataWatch(namespace: $namespace) {\n      type\n      object {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n": types.LogMetadataListWatchDocument,
};

/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 *
 *
 * @example
 * ```ts
 * const query = gql(`query GetUser($id: ID!) { user(id: $id) { name } }`);
 * ```
 *
 * The query argument is unknown!
 * Please regenerate the types.
 */
export function gql(source: string): unknown;

/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment LogMetadataListItemFragment on LogMetadata {\n    id\n    spec {\n      nodeName\n      namespace\n      podName\n      containerName\n      containerID\n    }\n    fileInfo {\n      size\n      lastModifiedAt\n    }\n  }\n"): (typeof documents)["\n  fragment LogMetadataListItemFragment on LogMetadata {\n    id\n    spec {\n      nodeName\n      namespace\n      podName\n      containerName\n      containerID\n    }\n    fileInfo {\n      size\n      lastModifiedAt\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query LogMetadataListFetch($namespace: String = \"\") {\n    logMetadataList(namespace: $namespace) {\n      items {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query LogMetadataListFetch($namespace: String = \"\") {\n    logMetadataList(namespace: $namespace) {\n      items {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription LogMetadataListWatch($namespace: String = \"\") {\n    logMetadataWatch(namespace: $namespace) {\n      type\n      object {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription LogMetadataListWatch($namespace: String = \"\") {\n    logMetadataWatch(namespace: $namespace) {\n      type\n      object {\n        ...LogMetadataListItemFragment\n      }\n    }\n  }\n"];

export function gql(source: string) {
  return (documents as any)[source] ?? {};
}

export type DocumentType<TDocumentNode extends DocumentNode<any, any>> = TDocumentNode extends DocumentNode<  infer TType,  any>  ? TType  : never;
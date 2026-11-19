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

import { createContext } from 'react';

import type { LogViewerHandle } from '@/components/widgets/log-viewer';

import type { LogServerClient } from './log-server-client';

/**
 * Page context
 */

type PageContextType = {
  kubeContext: string | null;
  shouldUseClusterAPI: boolean | undefined;
  logServerClient: LogServerClient | undefined;
  grep: string | null;
  logViewerRef: React.RefObject<LogViewerHandle | null>;
  isSidebarOpen: boolean;
  setIsSidebarOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export const PageContext = createContext({} as PageContextType);

/**
 * ViewerColumns
 */

export enum ViewerColumn {
  Timestamp = 'Timestamp',
  ColorDot = 'Color Dot',
  Pod = 'Pod',
  Container = 'Container',
  Region = 'Region',
  Zone = 'Zone',
  OS = 'OS',
  Arch = 'Arch',
  Node = 'Node',
  Message = 'Message',
}

export const ALL_VIEWER_COLUMNS = [
  ViewerColumn.Timestamp,
  ViewerColumn.ColorDot,
  ViewerColumn.Pod,
  ViewerColumn.Container,
  ViewerColumn.Region,
  ViewerColumn.Zone,
  ViewerColumn.OS,
  ViewerColumn.Arch,
  ViewerColumn.Node,
  ViewerColumn.Message,
];

const CONFIG_KEY_TO_VIEWER_COLUMN: Record<string, ViewerColumn> = {
  timestamp: ViewerColumn.Timestamp,
  dot: ViewerColumn.ColorDot,
  pod: ViewerColumn.Pod,
  container: ViewerColumn.Container,
  region: ViewerColumn.Region,
  zone: ViewerColumn.Zone,
  os: ViewerColumn.OS,
  arch: ViewerColumn.Arch,
  node: ViewerColumn.Node,
};

export function configColumnsToViewerColumns(configColumns: string[]): ViewerColumn[] {
  return configColumns.reduce<ViewerColumn[]>((cols, key) => {
    const col = CONFIG_KEY_TO_VIEWER_COLUMN[key];
    if (col) cols.push(col);
    return cols;
  }, []);
}

// Maps a viewer column to the backend's column name. Omits ColorDot (UI-only,
// no backing data) so callers can filter it out via `undefined`.
const VIEWER_COLUMN_TO_BACKEND: Partial<Record<ViewerColumn, string>> = {
  [ViewerColumn.Timestamp]: 'timestamp',
  [ViewerColumn.Pod]: 'pod',
  [ViewerColumn.Container]: 'container',
  [ViewerColumn.Region]: 'region',
  [ViewerColumn.Zone]: 'zone',
  [ViewerColumn.OS]: 'os',
  [ViewerColumn.Arch]: 'arch',
  [ViewerColumn.Node]: 'node',
  [ViewerColumn.Message]: 'message',
};

export function viewerColumnToBackend(col: ViewerColumn): string | undefined {
  return VIEWER_COLUMN_TO_BACKEND[col];
}

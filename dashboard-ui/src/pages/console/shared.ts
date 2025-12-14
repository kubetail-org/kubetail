// Copyright 2024-2025 Andres Morey
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

export type { LogRecordsFragmentFragment as LogRecord } from '@/lib/graphql/dashboard/__generated__/graphql';

/**
 * Page context
 */

type PageContextType = {
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
  PodContainer = 'Pod/Container',
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
  ViewerColumn.PodContainer,
  ViewerColumn.Region,
  ViewerColumn.Zone,
  ViewerColumn.OS,
  ViewerColumn.Arch,
  ViewerColumn.Node,
  ViewerColumn.Message,
];

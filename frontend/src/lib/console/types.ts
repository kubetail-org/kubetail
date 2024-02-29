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

import type { ApolloError } from '@apollo/client';

import type { ExtractQueryType } from '@/app-env';
import type { LogRecord as GraphQLLogRecord } from '@/lib/graphql/__generated__/graphql';
import * as fragments from '@/lib/graphql/fragments';

/*
export enum DurationUnit {
  Minutes = 'minutes',
  Hours = 'hours',
  Days = 'days',
  Weeks = 'weeks',
  Months = 'moths',
}

export class Duration {
  value: number;
  unit: DurationUnit;

  constructor(value: number, unit: DurationUnit) {
    this.value = value;
    this.unit = unit;
  }

  toISOString() {
    switch (this.unit) {
      case DurationUnit.Minutes:
        return `PT${this.value}M`;
      case DurationUnit.Hours:
        return `PT${this.value}H`;
      case DurationUnit.Days:
        return `P${this.value}D`;
      case DurationUnit.Weeks:
        return `P${this.value}W`;
      case DurationUnit.Months:
        return `P${this.value}M`;
    }
  }
}*/

export type Node = ExtractQueryType<typeof fragments.CONSOLE_NODES_LIST_ITEM_FRAGMENT>;
export type Workload = ExtractQueryType<typeof fragments.CONSOLE_LOGGING_RESOURCES_GENERIC_OBJECT_FRAGMENT>;
export type Pod = ExtractQueryType<typeof fragments.CONSOLE_LOGGING_RESOURCES_POD_FRAGMENT>;

export class WorkloadResponse {
  loading: boolean = false;
  error?: ApolloError = undefined;
  item?: Workload | null = undefined;
}

export class PodListResponse {
  loading: boolean = false;
  error?: ApolloError = undefined;
  items?: Pod[] | null = undefined;
};

export enum LogFeedState {
  Streaming = 'STREAMING',
  Paused = 'PAUSED',
  InQuery = 'IN_QUERY',
}

export type LogFeedQueryOptions = {
  since?: string;
  until?: string;
};

export enum LogFeedColumn {
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

export const allLogFeedColumns = [
  LogFeedColumn.Timestamp,
  LogFeedColumn.ColorDot,
  LogFeedColumn.PodContainer,
  LogFeedColumn.Region,
  LogFeedColumn.Zone,
  LogFeedColumn.OS,
  LogFeedColumn.Arch,
  LogFeedColumn.Node,
  LogFeedColumn.Message,
];

export interface LogRecord extends GraphQLLogRecord {
  node: Node;
  pod: Pod;
  container: string;
};
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

import { atom } from 'jotai';

import { MapSet } from '@/lib/util';
import type { LogSourceFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';

import { ViewerColumn } from './shared';

/**
 * UI state
 */

export const isFollowAtom = atom(true);

export const isWrapAtom = atom(false);

export const visibleColsAtom = atom(new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message]));

export const filtersAtom = atom(new MapSet<string, string>());

export const sourceMapAtom = atom(new Map<string, LogSourceFragmentFragment>());

export const sourcesAtom = atom((get) => Array.from(get(sourceMapAtom).values()));

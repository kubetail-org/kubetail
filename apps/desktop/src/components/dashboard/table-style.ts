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

// The look the dashboard's tables share, on the card around a `VirtualTable`.

// The header strip: muted, 12px muted labels.
export const TABLE_HEADER = 'bg-muted [&_th]:h-9 [&_th]:text-xs [&_th]:text-muted-foreground';
// Tighter than the library's cells: 12px between columns, 12px at the card's edges.
export const TABLE_CELLS =
  '[&_td]:px-1.5 [&_th]:px-1.5 [&_td:first-child]:pl-3 [&_th:first-child]:pl-3 [&_td:last-child]:pr-3 [&_th:last-child]:pr-3';

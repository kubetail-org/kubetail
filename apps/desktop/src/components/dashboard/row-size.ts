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

// The heights a `VirtualTable` row can take. They live apart from the component so that
// Fast Refresh, which reloads a module whose non-component exports change identity, keeps
// the table's state on an edit.

// One fact, two forms. `ROW_H_PX` is the pinned row's rendered height — `TableRow` has
// `border-b` under collapsed table borders, so it is read off a rendered row, not derived.
export const ROW_H = 'h-9';
export const ROW_H_PX = 36;

// A row's height, as a class and in pixels.
export type RowSize = { className: string; px: number };
export const ROW: RowSize = { className: ROW_H, px: ROW_H_PX };
// A row of two lines, such as a name over its owner. A row's height is only a minimum, and
// two lines under the library's cell padding would outgrow it, so its cells carry none.
export const TALL_ROW: RowSize = { className: 'h-11 [&>td]:py-0', px: 44 };

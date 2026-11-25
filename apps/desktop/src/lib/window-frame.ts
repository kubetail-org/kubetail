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

// The element `WindowFrame` draws the window in, for what has to stay inside its visible
// edge: on Linux an inset box, a gutter for its shadow around it; elsewhere none, the
// window being the whole viewport.
import { createContext, useContext } from 'react';

export const WindowFrameContext = createContext<HTMLElement | null>(null);

export function useWindowFrame(): HTMLElement | null {
  return useContext(WindowFrameContext);
}

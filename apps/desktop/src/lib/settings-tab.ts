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

// Selects a tab of the Settings dialog by its label, so one pane can send the
// user to another. Outside the dialog it does nothing.
import { createContext, useContext } from 'react';

export const SelectSettingsTab = createContext<(label: string) => void>(() => {});

export function useSelectSettingsTab(): (label: string) => void {
  return useContext(SelectSettingsTab);
}

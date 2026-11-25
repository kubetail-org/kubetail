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

// Space (px) reserved for the macOS traffic lights: the host places the first at
// x=20 (`window_manager.rs`) and the three end near x=72; the rest is breathing
// room. Eyeball against the native buttons when tuning.
const TRAFFIC_LIGHT_GUTTER = 76;

/** The macOS traffic lights' place at a bar's left end, in the app and log windows. */
export function TrafficLightGutter() {
  return (
    <div data-testid="traffic-light-gutter" aria-hidden className="shrink-0" style={{ width: TRAFFIC_LIGHT_GUTTER }} />
  );
}

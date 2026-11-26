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

// A query re-runs for nobody, so one that failed while the sidecar was unreachable
// stays failed after it comes back. A watch going live is the evidence it answers
// again, so a failure is asked again for once per live watch — bounded, because
// asking clears the failure and the answer sets it, which unbounded is a request
// loop.
import { useEffect, useRef } from 'react';

export function useAskAgainWhenLive(failed: boolean, live: boolean, retry: () => void) {
  const asked = useRef(false);
  useEffect(() => {
    if (!live) {
      asked.current = false;
      return;
    }
    if (failed && !asked.current) {
      asked.current = true;
      retry();
    }
  }, [failed, live, retry]);
}

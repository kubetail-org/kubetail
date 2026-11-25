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

import { Button } from '@kubetail/ui/elements/button';
import { RotateCw } from 'lucide-react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';

const ClusterConnectionRetryMutation = graphql(`
  mutation ClusterConnectionRetry($id: ClusterID!) {
    clusterConnectionRetry(id: $id)
  }
`);

// Forces an immediate re-probe, resetting the backoff. The mutation resolves when the probe
// it asked for has finished, so `fetching` is the answer to "is my re-probe still out": no
// timer, and the button agrees with `NextCheck`'s "checking…" because both follow the same
// run. A failure needs no handling: the client's errorReportExchange puts every operation
// error on the bus. A press holds the button as `retryingLabel`; `checking`, a probe the caller
// sees running whoever asked for it, holds it as `checkingLabel`.
export function RetryButton({
  clusterId,
  label = 'Retry now',
  size = 'sm',
  retryingLabel = 'Retrying…',
  checking = false,
  checkingLabel = 'Checking…',
}: {
  clusterId: string;
  label?: string;
  size?: 'xs' | 'sm';
  retryingLabel?: string;
  checking?: boolean;
  checkingLabel?: string;
}) {
  const [{ fetching }, retry] = useMutation(ClusterConnectionRetryMutation);
  const busy = fetching || checking;
  let text = label;
  if (fetching) text = retryingLabel;
  else if (checking) text = checkingLabel;
  return (
    <Button
      type="button"
      variant="outline"
      size={size}
      disabled={busy}
      onClick={() => {
        void retry({ id: clusterId });
      }}
    >
      <RotateCw className={`size-3.5 ${busy ? 'animate-spin' : ''}`} aria-hidden />
      {text}
    </Button>
  );
}

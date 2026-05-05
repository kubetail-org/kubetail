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

import { useSubscription } from '@apollo/client/react';
import { atom, useAtomValue, useSetAtom } from 'jotai';
import { useEffect } from 'react';

import appConfig from '@/app-config';
import { KubeConfigFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { KUBE_CONFIG_WATCH } from '@/lib/graphql/dashboard/ops';

const IS_DESKTOP = appConfig.environment === 'desktop';

type KubeConfigState = { data: KubeConfigFragmentFragment | null; loading: boolean };

export const kubeConfigAtom = atom<KubeConfigState>({ data: null, loading: IS_DESKTOP });

/** Mounts once at the app root. Owns the single KUBE_CONFIG_WATCH subscription. */
export function KubeConfigEffect() {
  const setState = useSetAtom(kubeConfigAtom);
  const { data, loading } = useSubscription(KUBE_CONFIG_WATCH, { skip: !IS_DESKTOP });

  useEffect(() => {
    const nextData = data?.kubeConfigWatch?.object ?? null;
    setState((prev) => (prev.data === nextData && prev.loading === loading ? prev : { data: nextData, loading }));
  }, [data, loading, setState]);

  return null;
}

export function useKubeConfig(): KubeConfigState {
  return useAtomValue(kubeConfigAtom);
}

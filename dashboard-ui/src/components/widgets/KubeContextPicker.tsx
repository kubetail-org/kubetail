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
import { useEffect } from 'react';

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';

import { KUBE_CONFIG_WATCH } from '@/lib/graphql/dashboard/ops';

/**
 * KubeContextPicker component
 */

type KubeContextPickerProps = {
  className?: string;
  value: string | null;
  setValue: (value: string) => void;
};

const KubeContextPicker = ({ className, value, setValue }: KubeContextPickerProps) => {
  const { loading, data } = useSubscription(KUBE_CONFIG_WATCH);
  const kubeConfig = data?.kubeConfigWatch?.object;

  // Set default value
  useEffect(() => {
    const kubeContext = kubeConfig?.currentContext;
    if (kubeContext !== undefined) setValue(kubeContext);
  }, [kubeConfig !== undefined]);

  return (
    <Select value={value || ''} onValueChange={(v) => setValue(v)} disabled={loading}>
      <SelectTrigger className={className}>
        <SelectValue placeholder="Loading..." />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Clusters</SelectLabel>
          {kubeConfig &&
            kubeConfig.contexts.map((context) => (
              <SelectItem key={context.name} value={context.name}>
                {context.name}
              </SelectItem>
            ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
};

export default KubeContextPicker;

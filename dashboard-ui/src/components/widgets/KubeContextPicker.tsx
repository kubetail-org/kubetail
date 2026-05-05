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

import { useKubeConfig } from '@/lib/kubeconfig';

/**
 * KubeContextPicker component
 */

type KubeContextPickerProps = {
  className?: string;
  value: string | null;
  setValue: (value: string) => void;
};

const KubeContextPicker = ({ className, value, setValue }: KubeContextPickerProps) => {
  const { data, loading } = useKubeConfig();
  const currentContext = data?.currentContext ?? null;
  const names = (() => {
    const all = data?.contexts?.map((c) => c.name) ?? [];
    if (!currentContext || !all.includes(currentContext)) return all;
    return [currentContext, ...all.filter((n) => n !== currentContext)];
  })();

  // Default to the kubeconfig's currentContext (falling back to the first name) once data arrives.
  useEffect(() => {
    if (value !== null) return;
    if (currentContext) setValue(currentContext);
    else if (names.length > 0) setValue(names[0]);
  }, [value, currentContext, names, setValue]);

  return (
    <Select value={value || ''} onValueChange={(v) => setValue(v)} disabled={loading}>
      <SelectTrigger className={className}>
        <SelectValue placeholder="Loading..." />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Clusters</SelectLabel>
          {names.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
};

export default KubeContextPicker;

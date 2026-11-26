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

// The dashboard's namespace scope: the `namespace` search param on `_app`, undefined for
// every namespace. See docs/adr/2026-11-12-the-namespace-scope-is-a-search-param.md
import { useNavigate, useSearch } from '@tanstack/react-router';

export function useNamespaceScope(): { namespace: string | undefined; setNamespace: (ns: string | undefined) => void } {
  const { namespace } = useSearch({ strict: false });
  const navigate = useNavigate();
  const setNamespace = (ns: string | undefined) => {
    void navigate({ to: '.', search: (prev) => ({ ...prev, namespace: ns }) });
  };
  return { namespace, setNamespace };
}

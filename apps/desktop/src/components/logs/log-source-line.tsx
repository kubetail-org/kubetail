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
// How a view is named in words, shared by the transcript's card and the right
// sidebar's header: the enum's word is the app's own, every name the cluster's.
import { VisibleText } from '@/components/primitives/visible-text';
import type { LogsViewAction } from '@/lib/logs-view';

type Source = LogsViewAction['sources'][number];

// Names from the cluster or the model, comma-separated, each spelled through
// VisibleText.
export function Names({ names }: { names: string[] }) {
  return names.map((name, i) => (
    // oxlint-disable-next-line react/no-array-index-key -- names can repeat, and the list is drawn whole
    <span key={i}>
      {i > 0 && ', '}
      <VisibleText text={name} />
    </span>
  ));
}

// `Deployment webapp in prod (app, istio-proxy), previous instance`. A source
// naming no container reads each pod's default, which needs no words.
export function SourceLine({ source }: { source: Source }) {
  return (
    <>
      {source.kind} <VisibleText text={source.name} /> in <VisibleText text={source.namespace} />
      {source.containers.length > 0 && (
        <>
          {' ('}
          <Names names={source.containers} />)
        </>
      )}
      {source.allContainers && ' (every container)'}
      {source.previous && ', previous instance'}
    </>
  );
}

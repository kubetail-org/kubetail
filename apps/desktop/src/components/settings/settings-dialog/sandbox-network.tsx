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

// The Sandbox pane's Network: what a sandboxed command can reach.
import { VisibleText } from '@/components/primitives/visible-text';
import { CLOUD_PLATFORM_NAMES } from '@/lib/chats';
import { useSandboxCloudAccess } from '@/lib/sandbox-cloud-access';

import { Group, NoteRow } from './group';

const list = new Intl.ListFormat('en', { type: 'conjunction' });

export function SandboxNetwork({
  networkAvailable,
  networkReason,
}: {
  networkAvailable: boolean | undefined;
  networkReason: string;
}) {
  const { platforms: cloud, readFailed } = useSandboxCloudAccess();
  return (
    <Group title="Network" lede="What a sandboxed command can reach.">
      <ul className="divide-y rounded-lg border">
        <NoteRow label="Your clusters">Through Kubetail, under your Permissions settings.</NoteRow>
        <NoteRow label="Cloud platforms">
          {readFailed && "Couldn't load cloud access."}
          {cloud &&
            (cloud.length > 0
              ? `${list.format(cloud.map((target) => CLOUD_PLATFORM_NAMES[target]))}, through Kubetail. Your sign-in stays outside.`
              : 'None. Every cloud platform is off.')}
        </NoteRow>
        <NoteRow label="Everything else">
          {networkAvailable ? (
            'Off by default. You can allow the internet for a chat or a command.'
          ) : (
            <span className="break-all whitespace-pre-wrap" data-testid="network-reason">
              <VisibleText text={networkReason} trailing="end" />
            </span>
          )}
        </NoteRow>
      </ul>
    </Group>
  );
}

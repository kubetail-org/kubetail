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

// The composer's add-context button. Its popup will list what a message can carry.
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Plus } from 'lucide-react';

export function ContextPopover() {
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button type="button" variant="ghost" size="icon" aria-label="Add context" className="shrink-0 rounded-lg" />
        }
      >
        <Plus aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="w-72">
        <p className="text-sm font-medium">Add context</p>
        <p className="text-sm text-muted-foreground">Context sources are coming soon.</p>
      </PopoverContent>
    </Popover>
  );
}

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

// The Mode picker: a tile per color scheme, each previewing it.
import { Radio } from '@base-ui/react/radio';
import { RadioGroup } from '@base-ui/react/radio-group';
import { cn } from '@kubetail/ui/lib/utils';

import { type ColorSchemePreference, useColorScheme } from '@/lib/theme';

// A preview draws its scheme whatever the app's is, so its colors are fixed: a
// card on the scheme's background. System is Light on the left and Dark on the
// right, the card crossing the split. "System" leads because it's the default.
const SCHEMES: { value: ColorSchemePreference; label: string; background: string; card: string }[] = [
  {
    value: 'system',
    label: 'System',
    background: 'bg-[linear-gradient(to_right,#f4f4f5_50%,#18181b_50%)]',
    card: 'bg-[linear-gradient(to_right,#ffffff_50%,#3f3f46_50%)]',
  },
  { value: 'light', label: 'Light', background: 'bg-[#f4f4f5]', card: 'bg-white' },
  { value: 'dark', label: 'Dark', background: 'bg-[#18181b]', card: 'bg-[#3f3f46]' },
];

export function ColorSchemePicker() {
  const { preference, setPreference } = useColorScheme();
  return (
    <RadioGroup aria-label="Mode" className="flex gap-3" value={preference} onValueChange={setPreference}>
      {SCHEMES.map(({ value, label, background, card }) => (
        <Radio.Root
          key={value}
          value={value}
          className="group flex flex-col items-center gap-1.5 rounded-md outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          {/* The card is centered, so the picked tile's wider border does not shift it. */}
          <span
            className={cn(
              'flex h-13 w-20 items-center justify-center overflow-hidden rounded-md border border-border group-data-checked:border-2 group-data-checked:border-primary',
              background,
            )}
          >
            <span className={cn('h-8 w-14 rounded', card)} />
          </span>
          <span className="text-xs text-muted-foreground group-data-checked:font-medium group-data-checked:text-popover-foreground">
            {label}
          </span>
        </Radio.Root>
      ))}
    </RadioGroup>
  );
}

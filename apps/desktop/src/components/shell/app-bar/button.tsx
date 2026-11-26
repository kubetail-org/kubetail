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

// The look every button in the app bar wears, in one place so they cannot drift apart.
import { Button } from '@kubetail/ui/elements/button';
import { cn } from '@kubetail/ui/lib/utils';
import type { ComponentProps } from 'react';

import { APP_BAR_CAPSULE_CLASS, APP_BAR_HOVER_CLASS } from '@/lib/control-style';

// `ghost` has no resting fill, so the bar's tint shows through and only hover paints;
// `icon-sm` is 28px, drawn round to match the bar's capsules and omnibox. The icon is gray.
const VARIANTS = { variant: 'ghost', size: 'icon-sm' } as const;

export function AppBarButton({ className, ...props }: ComponentProps<typeof Button>) {
  return (
    <Button
      {...VARIANTS}
      className={cn('rounded-full text-muted-foreground', APP_BAR_HOVER_CLASS, className)}
      {...props}
    />
  );
}

// Groups related app bar buttons in one track.
export function AppBarCapsule({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn(APP_BAR_CAPSULE_CLASS, className)} {...props} />;
}

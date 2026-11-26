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
import { Check, Copy } from 'lucide-react';
import { useEffect, useState } from 'react';

import { copyToClipboard } from '@/lib/clipboard';
import { SMALL_ICON_STROKE } from '@/lib/control-style';

// How long the button shows the check after a click.
const COPIED_FOR_MS = 1_500;

// A small ghost button that copies `text`. The check stays `COPIED_FOR_MS` after
// the latest click; the count re-arms the timer on every one.
export function CopyButton({
  text,
  label,
  disabled = false,
  className,
}: {
  text: string;
  label: string;
  disabled?: boolean;
  className?: string;
}) {
  const [copies, setCopies] = useState(0);
  useEffect(() => {
    if (copies === 0) return undefined;
    const timer = setTimeout(() => setCopies(0), COPIED_FOR_MS);
    return () => clearTimeout(timer);
  }, [copies]);

  const copy = () => {
    copyToClipboard(text);
    setCopies((n) => n + 1);
  };
  const copied = copies > 0;
  const Icon = copied ? Check : Copy;

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      aria-label={copied ? 'Copied' : label}
      disabled={disabled}
      className={className}
      onClick={copy}
    >
      <Icon aria-hidden="true" strokeWidth={SMALL_ICON_STROKE} />
    </Button>
  );
}

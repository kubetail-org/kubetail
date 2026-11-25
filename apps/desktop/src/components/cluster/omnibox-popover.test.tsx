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

import { Popover as PopoverPrimitive } from '@base-ui/react/popover';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ComponentProps } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { WindowFrameContext } from '@/lib/window-frame';

// The positioner's collision props, as the popover gave them; jsdom has no layout to
// show what they do.
const { positionerProps } = vi.hoisted(() => ({ positionerProps: { current: {} } }));
vi.mock('@base-ui/react/popover', async (importOriginal) => {
  const { Popover } = await importOriginal<typeof import('@base-ui/react/popover')>();
  const Positioner = (props: ComponentProps<typeof Popover.Positioner>) => {
    positionerProps.current = props;
    return <Popover.Positioner {...props} />;
  };
  return { Popover: { ...Popover, Positioner } };
});

const { OmniboxPopover } = await import('./omnibox-popover');

function renderPopover(frame: HTMLElement | null = null) {
  return render(
    <WindowFrameContext value={frame}>
      <div data-testid="host">
        <OmniboxPopover
          anchorRef={{ current: null }}
          popup={<PopoverPrimitive.Popup>Popup body</PopoverPrimitive.Popup>}
        >
          <button type="button">Open</button>
        </OmniboxPopover>
      </div>
    </WindowFrameContext>,
  );
}

describe('OmniboxPopover', () => {
  it('opens on the trigger’s click, in a portal above the sidebar drawer', async () => {
    const user = userEvent.setup();
    renderPopover();
    expect(screen.queryByText('Popup body')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Open' }));
    const body = await screen.findByText('Popup body');
    expect(screen.getByTestId('host').contains(body)).toBe(false);
    expect(body.closest('.z-50')).not.toBeNull();
  });

  // On Linux the frame sits inset in the viewport, its shadow's gutter around it.
  it('keeps 8px inside the window frame, or the viewport where there is none', async () => {
    const user = userEvent.setup();
    const { unmount } = renderPopover();
    await user.click(screen.getByRole('button', { name: 'Open' }));
    expect(positionerProps.current).toMatchObject({ collisionBoundary: 'clipping-ancestors', collisionPadding: 8 });
    unmount();

    const frame = document.createElement('div');
    renderPopover(frame);
    await user.click(screen.getByRole('button', { name: 'Open' }));
    expect(positionerProps.current).toMatchObject({ collisionBoundary: frame, collisionPadding: 8 });
  });

  it('closes on Escape', async () => {
    const user = userEvent.setup();
    renderPopover();
    await user.click(screen.getByRole('button', { name: 'Open' }));
    await screen.findByText('Popup body');

    await user.keyboard('{Escape}');
    expect(screen.queryByText('Popup body')).not.toBeInTheDocument();
  });
});

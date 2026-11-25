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

import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { AppMark } from './app-mark';

describe('AppMark', () => {
  afterEach(cleanup);

  it('draws the light and dark icons inline, decoratively, at the size given', () => {
    render(<AppMark size={48} />);
    const mark = screen.getByTestId('app-mark');
    expect(mark).toHaveAttribute('aria-hidden', 'true');
    expect(mark).toHaveClass('size-12');
    const [light, dark] = mark.querySelectorAll('svg');
    expect(light.querySelector('path')).toHaveAttribute('fill', '#9F5436');
    expect(light).toHaveClass('dark:hidden');
    expect(dark.querySelector('path')).toHaveAttribute('fill', '#DEC1B7');
    expect(dark).toHaveClass('hidden', 'dark:block');
    [light, dark].forEach((svg) => {
      expect(svg).toHaveAttribute('width', '48');
      expect(svg).toHaveAttribute('height', '48');
    });
  });

  it('draws the sidebar size', () => {
    render(<AppMark size={24} />);
    const mark = screen.getByTestId('app-mark');
    expect(mark).toHaveClass('size-6');
    const svgs = mark.querySelectorAll('svg');
    expect(svgs).toHaveLength(2);
    svgs.forEach((svg) => {
      expect(svg).toHaveAttribute('width', '24');
      expect(svg).toHaveAttribute('height', '24');
    });
  });
});

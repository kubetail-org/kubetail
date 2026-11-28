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

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { DetailCard } from './detail-card';

describe('DetailCard', () => {
  it('heads its body with a title', () => {
    render(<DetailCard title="Details">body</DetailCard>);
    const card = screen.getByRole('heading', { level: 3, name: 'Details' }).closest('section')!;
    expect(card).toHaveTextContent('Detailsbody');
    expect(card.querySelector('header')).toHaveClass('h-10', 'border-b', 'bg-card', 'px-3');
  });

  it('draws a note on the header’s right, a zero included, and none when absent', () => {
    const { rerender } = render(
      <DetailCard title="Labels" note={0}>
        body
      </DetailCard>,
    );
    expect(screen.getByText('0')).toHaveClass('text-xs', 'text-muted-foreground');
    rerender(<DetailCard title="Labels">body</DetailCard>);
    expect(screen.queryByText('0')).not.toBeInTheDocument();
  });
});

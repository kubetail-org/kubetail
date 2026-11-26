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

import { PageHeader } from './page-header';

describe('PageHeader', () => {
  it('draws its note, and no title: the breadcrumb bar names the page', () => {
    render(<PageHeader>Events in checkout.</PageHeader>);
    expect(screen.getByText('Events in checkout.')).toHaveClass('text-sm', 'text-muted-foreground');
    expect(screen.queryByRole('heading')).not.toBeInTheDocument();
  });

  it('draws nothing without a note', () => {
    const { container } = render(<PageHeader>{false}</PageHeader>);
    expect(container).toBeEmptyDOMElement();
  });
});

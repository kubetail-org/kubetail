// Copyright 2024 Andres Morey
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

import { render } from '@testing-library/react';
import { MemoryRouter, Routes } from 'react-router-dom';

import { routes } from './routes';

vi.mock('@/pages/home', () => ({
  default: () => <div>Home</div>,
}));

vi.mock('@/pages/console', () => ({
  default: () => <div>Console</div>,
}));

vi.mock('@/pages/auth/login', () => ({
  default: () => <div>Auth-Login</div>,
}));

vi.mock('@/pages/auth/logout', () => ({
  default: () => <div>Auth-Logout</div>,
}));

const renderPage = (path: string) => (
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        {routes}
      </Routes>
    </MemoryRouter>,
  )
);

describe('route tests', () => {
  it('/', () => {
    const { getByText } = renderPage('/');
    expect(getByText('Home')).toBeInTheDocument();
  });

  it('/console', () => {
    const { getByText } = renderPage('/console');
    expect(getByText('Console')).toBeInTheDocument();
  });

  it('/auth/login', () => {
    const { getByText } = renderPage('/auth/login');
    expect(getByText('Auth-Login')).toBeInTheDocument();
  });

  it('/auth/logout', () => {
    const { getByText } = renderPage('/auth/logout');
    expect(getByText('Auth-Logout')).toBeInTheDocument();
  });
});

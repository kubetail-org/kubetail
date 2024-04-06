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

import { act, fireEvent, render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Mock } from 'vitest';

import ProfilePicDropdown from '@/components/widgets/ProfilePicDropdown';
import { useSession } from '@/lib/auth';

describe('ProfilePicDropdown component tests', () => {
  it('should render profile pic with hidden menu by default', () => {
    // configure mock
    (useSession as Mock).mockReturnValue({
      session: { user: 'test-user' },
    });

    const { getByTitle, getByText } = render(<ProfilePicDropdown />);

    // assertions
    expect(getByTitle('test-user')).toBeInTheDocument();
    expect(getByText('Open user menu')).toBeInTheDocument();
  });

  it('should render menu when profile pic is clicked', async () => {
    // configure mock
    (useSession as Mock).mockReturnValue({
      session: { user: 'test-user' },
    });

    const { getByRole, getByText } = render(
      <MemoryRouter>
        <ProfilePicDropdown />
      </MemoryRouter>,
    );

    const buttonEl = getByRole('button');

    // check that button exists
    expect(buttonEl).toBeInTheDocument();

    // click on button
    await act(async () => {
      fireEvent.click(buttonEl);
    });

    // assertions
    expect(getByText('User: test-user')).toBeInTheDocument();
    expect(getByText('Sign out')).toBeInTheDocument();
  });

  it('should not render signout link when user is `local`', async () => {
    // configure mock
    (useSession as Mock).mockReturnValue({
      session: { user: 'local' },
    });

    const { getByRole, queryByText } = render(
      <MemoryRouter>
        <ProfilePicDropdown />
      </MemoryRouter>,
    );

    const buttonEl = getByRole('button');

    // check that button exists
    expect(buttonEl).toBeInTheDocument();

    // click on button
    await act(async () => {
      fireEvent.click(buttonEl);
    });

    // assertions
    expect(queryByText('Sign out')).not.toBeInTheDocument();
  });
});

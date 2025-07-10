// Copyright 2024-2025 Andres Morey
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

import { Menu, MenuButton, MenuItems, MenuItem, Transition } from '@headlessui/react';
import { UserCircleIcon } from '@heroicons/react/24/solid';
import { Fragment } from 'react';
import { Link } from 'react-router-dom';

import { useSession } from '@/lib/auth';

const ProfilePicDropdown = () => {
  const { session } = useSession();

  return (
    <Menu as="div" className="ml-3 relative">
      <div>
        <MenuButton className="max-w-xs bg-background flex items-center text-sm rounded-full focus:outline-hidden">
          <span className="sr-only">Open user menu</span>
          <UserCircleIcon
            className="h-11 w-11 fill-chrome-400 hover:fill-chrome-600 bg-chrome-100"
            aria-hidden="true"
            title={session?.user || ''}
          />
        </MenuButton>
      </div>
      <Transition
        as={Fragment}
        enter="transition ease-out duration-100"
        enterFrom="transform opacity-0 scale-95"
        enterTo="transform opacity-100 scale-100"
        leave="transition ease-in duration-75"
        leaveFrom="transform opacity-100 scale-100"
        leaveTo="transform opacity-0 scale-95"
      >
        <MenuItems className="origin-top-right absolute right-0 mt-2 w-48 rounded-md shadow-lg py-1 bg-background ring-1 ring-black ring-opacity-5 focus:outline-hidden">
          <MenuItem>
            <div className="px-4 py-2 text-sm font-semibold text-chrome-700 border-b">{`User: ${session?.user}`}</div>
          </MenuItem>
          {['cluster', 'local'].includes(session?.user || '') === false && (
            <MenuItem>
              <Link
                className="block px-4 py-2 text-sm text-chrome-700 cursor-pointer"
                to={`/auth/logout?${new URLSearchParams({ callbackUrl: window.location.pathname + window.location.search })}`}
              >
                Sign out
              </Link>
            </MenuItem>
          )}
        </MenuItems>
      </Transition>
    </Menu>
  );
};

export default ProfilePicDropdown;

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

import { Settings } from 'lucide-react';
import { useLayoutEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';

import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';

import appConfig from '@/app-config';
import { ClusterSettingsDialog } from '@/components/widgets/ClusterSettingsDialog';

const SettingsDropdown = () => {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [width, setWidth] = useState<number>();

  const [isDialogOpen, setIsDialogOpen] = useState(false);

  useLayoutEffect(() => {
    if (triggerRef.current) {
      setWidth(triggerRef.current.offsetWidth);
    }
  }, []);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button ref={triggerRef} size="sm" variant="outline" className="bg-transparent mb-2">
            <Settings size={18} strokeWidth={1.5} />
            Settings
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" style={{ width }}>
          <DropdownMenuGroup>
            <DropdownMenuItem onSelect={() => setIsDialogOpen(true)}>Cluster Settings</DropdownMenuItem>
          </DropdownMenuGroup>
          {appConfig.authMode === 'token' && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem>
                <Link
                  to={`/auth/logout?${new URLSearchParams({ callbackUrl: window.location.pathname + window.location.search })}`}
                >
                  Sign out
                </Link>
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <ClusterSettingsDialog isOpen={isDialogOpen} onClose={() => setIsDialogOpen(false)} />
    </>
  );
};

export default SettingsDropdown;

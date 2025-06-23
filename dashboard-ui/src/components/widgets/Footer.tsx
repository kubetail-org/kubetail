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

import Form from '@kubetail/ui/elements/Form';

import ServerStatus from '@/components/widgets/ServerStatus';
import { useTheme, UserPreference } from '@/lib/theme';
import EnvironmentControl from './EnvironmentControl';

export default function Footer() {
  const { userPreference, setUserPreference } = useTheme();

  const handleChange = (ev: React.ChangeEvent<HTMLSelectElement>) => {
    setUserPreference(ev.target.value as UserPreference);
  };

  return (
    <div className="h-[22px] bg-chrome-100 border-t border-chrome-divider text-sm flex justify-between items-center pl-[10px]">
      <Form.Select
        className="w-[65px] mt-0 py-0 pl-0 pr-0 h-auto border-0 focus:ring-offset-0 focus:ring-0 focus:border-transparent focus:ring-transparent text-xs bg-transparent"
        value={userPreference}
        onChange={handleChange}
      >
        <Form.Option value={UserPreference.System}>system</Form.Option>
        <Form.Option value={UserPreference.Dark}>dark</Form.Option>
        <Form.Option value={UserPreference.Light}>light</Form.Option>
      </Form.Select>
      <div className="flex">
        {import.meta.env.MODE === 'development' && <EnvironmentControl />}
        <ServerStatus />
      </div>
    </div>
  );
}

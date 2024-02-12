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

import { XCircleIcon } from '@heroicons/react/24/outline';
import { Toaster, resolveValue } from 'react-hot-toast';
import { Outlet } from 'react-router-dom';

const CustomToaster = () => (
  <Toaster position="bottom-center">
    {(t) => (
      <div className="rounded-md bg-red-50 p-4">
        <div className="flex">
          <div className="flex-shrink-0">
            <XCircleIcon className="h-5 w-5 text-red-400" aria-hidden="true" />
          </div>
          <div className="ml-3">
            <h3 className="text-sm font-medium text-red-800">
              Error
            </h3>
            <div className="mt-2 text-sm text-red-700">
              {resolveValue(t.message, t)}
            </div>
          </div>
        </div>
      </div>
    )}
  </Toaster>
);

export default function Root() {
  return (
    <>
      <CustomToaster />
      <Outlet />
    </>
  );
}

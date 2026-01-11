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

import { useEffect } from 'react';
import { Link } from 'react-router-dom';

import logo from '@/assets/logo.svg';
import { cn } from '@/lib/util';

interface ModalLayoutProps extends React.PropsWithChildren {
  innerClassName?: string;
}

export default function ModalLayout({ children, innerClassName }: ModalLayoutProps) {
  useEffect(() => {
    const htmlEl = document.documentElement;
    const bodyEl = document.body;

    htmlEl.classList.add('h-full');
    bodyEl.classList.add('h-full', 'bg-chrome-50');

    return function cleanup() {
      htmlEl.classList.remove('h-full');
      bodyEl.classList.remove('h-full', 'bg-chrome-50');
    };
  });

  return (
    <div className="min-h-full pt-10 sm:px-6 lg:px-8">
      <div className={cn('sm:mx-auto', innerClassName !== undefined ? innerClassName : 'sm:w-full sm:max-w-md')}>
        <div className="flex justify-center">
          <Link to="/">
            <img className="mx-auto h-12 w-auto" src={logo} width={70} height={70} alt="KubeTail" />
          </Link>
        </div>
        <div>{children}</div>
      </div>
    </div>
  );
}

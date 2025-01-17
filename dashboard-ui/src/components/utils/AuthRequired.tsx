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

import { isValidElement } from 'react';
import { useLocation, Navigate } from 'react-router-dom';

import LoadingPage from '@/components/utils/LoadingPage';
import { useSession } from '@/lib/auth';

const AuthRequired = ({ children = null }: React.PropsWithChildren) => {
  const { session } = useSession();
  const location = useLocation();

  // render loading page
  if (session === undefined) {
    return <LoadingPage />;
  }

  // redirect to login page
  if (!session.user) {
    const callbackUrl = location.pathname + location.search;
    return <Navigate to={`/auth/login?${new URLSearchParams({ callbackUrl })}`} />;
  }

  // render contents
  if (isValidElement(children)) return children;
  return children;
};

export default AuthRequired;

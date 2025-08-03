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

import { useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';

import LoadingPage from '@/components/utils/LoadingPage';
import { getSession, useSession } from '@/lib/auth';

export default function Logout() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { loading, session } = useSession();

  // redirect if login status changes
  useEffect(() => {
    if (!loading && session?.user === null) navigate(searchParams.get('callbackUrl') || '/');
  }, [loading, session?.user]);

  useEffect(() => {
    (async () => {
      const url = new URL('/api/auth/logout', window.location.origin);
      const resp = await fetch(url, {
        method: 'post',
      });

      // update session
      if (resp.ok) await getSession();
    })();
  }, []);

  return <LoadingPage />;
}

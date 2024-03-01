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

import { Route } from 'react-router-dom';

import Root from '@/pages/_root';
import Login from '@/pages/auth/login';
import Logout from '@/pages/auth/logout';
import Console from '@/pages/console';
import Console2 from '@/pages/console2';
import Home from '@/pages/home';
import ErrorPage from '@/error-page';

import ScratchPage from '@/pages/scratch';

export const routes = (
  <Route path="/" element={<Root />} errorElement={<ErrorPage />}>
    <Route index element={<Home />} />
    <Route path="console" element={<Console />} />
    <Route path="console2" element={<Console2 />} />
    <Route path="scratch" element={<ScratchPage />} />
    <Route path="auth">
      <Route path="login" element={<Login />} />
      <Route path="logout" element={<Logout />} />
    </Route>
  </Route>
);

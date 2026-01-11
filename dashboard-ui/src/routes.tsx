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

import { Route } from 'react-router-dom';

import Root from '@/pages/_root';
import ErrorPage from '@/error-page';
import Login from '@/pages/auth/login';
import Logout from '@/pages/auth/logout';
import Home from '@/pages/home';
import Console from '@/pages/console';

export const routes = (
  <Route path="/" element={<Root />} errorElement={<ErrorPage />}>
    <Route index element={<Home />} />
    <Route path="console" element={<Console />} />
    <Route path="auth">
      <Route path="login" element={<Login />} />
      <Route path="logout" element={<Logout />} />
    </Route>
  </Route>
);

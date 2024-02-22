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

import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';

import Button from 'kubetail-ui/elements/Button';
import Form from 'kubetail-ui/elements/Form';
import Spinner from 'kubetail-ui/elements/Spinner';

import ModalLayout from '@/components/layouts/ModalLayout';
import LoadingPage from '@/components/utils/LoadingPage';
import { getSession, useSession } from '@/lib/auth';
import { getCSRFToken } from '@/lib/helpers';

type LoginFormElement = HTMLFormElement & {
  token: HTMLInputElement;
};

export default function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { loading, session } = useSession();
  const [formErrors, setFormErrors] = useState({} as Record<string, string>);
  const [serverLoading, setServerLoading] = useState(false);

  // redirect if login status changes
  useEffect(() => {
    if (!loading && session?.user !== null) navigate(searchParams.get('callbackUrl') || '/');
  }, [loading, session?.user]);

  // handle form submit
  const handleSubmit = async (ev: React.FormEvent<LoginFormElement>) => {
    ev.preventDefault();

    const formEl = ev.currentTarget;

    setServerLoading(true);
    const url = new URL('/api/auth/login', window.location.origin);
    const resp = await fetch(url, {
      method: 'post',
      headers: { 'X-CSRF-Token': await getCSRFToken() },
      body: new FormData(formEl),
    });

    // update session and exit
    if (resp.ok) {
      await getSession();
      setServerLoading(false);
      return;
    }
    setServerLoading(false);

    // update form errors
    try {
      const { errors } = await resp.json();
      setFormErrors(errors || {});
    } catch (err) {
      console.log(err);
      setFormErrors({});
    }
  }

  if (!session) return <LoadingPage />;

  return (
    <ModalLayout>
      <h2 className="block mt-6 text-center text-3xl font-extrabold text-chrome-900">
        Sign into Kubernetes
      </h2>
      <div className="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
        <div className="bg-white py-8 px-4 shadow sm:rounded-lg sm:px-10 relative">
          <Form onSubmit={handleSubmit}>
            <Form.Group>
              <Form.Label>Token</Form.Label>
              <Form.Control name="token" placeholder="Enter your kubernetes token..."></Form.Control>
              {formErrors.token && <Form.Control.Feedback>{formErrors.token}</Form.Control.Feedback>}
            </Form.Group>
            <Form.Group>
              <Button type="submit">Sign in</Button>
            </Form.Group>
          </Form>
          {serverLoading && (
            <div className="absolute top-0 left-0 w-full h-full bg-white bg-opacity-80 flex justify-center items-center">
              <div className="bg-white py-4 px-6 border border-chrome-200 flex">
                <Spinner size="sm" /><span>Loading...</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </ModalLayout>
  );
}

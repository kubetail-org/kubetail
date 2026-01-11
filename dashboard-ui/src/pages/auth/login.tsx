// Copyright 2024-2026 The Kubetail Authors
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

import { zodResolver } from '@hookform/resolvers/zod';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { z } from 'zod';

import { Button } from '@kubetail/ui/elements/button';
import { Input } from '@kubetail/ui/elements/input';
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@kubetail/ui/elements/form';
import { Spinner } from '@kubetail/ui/elements/spinner';

import ModalLayout from '@/components/layouts/ModalLayout';
import LoadingPage from '@/components/utils/LoadingPage';
import { getSession, useSession } from '@/lib/auth';

const loginForm = z.object({
  token: z.string().min(1, 'Please enter a token'),
});

type LoginForm = z.infer<typeof loginForm>;

export default function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { loading, session } = useSession();
  const [serverLoading, setServerLoading] = useState(false);

  const form = useForm<LoginForm>({
    resolver: zodResolver(loginForm),
    defaultValues: {
      token: '',
    },
  });

  // redirect if login status changes
  useEffect(() => {
    if (!loading && session?.user !== null) navigate(searchParams.get('callbackUrl') || '/');
  }, [loading, session?.user]);

  if (!session) return <LoadingPage />;

  const onSubmit = async (values: LoginForm) => {
    form.clearErrors();

    setServerLoading(true);
    const url = new URL('/api/auth/login', window.location.origin);
    const resp = await fetch(url, {
      method: 'post',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(values),
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
      const { errors }: { errors: LoginForm } = await resp.json();
      form.setError('token', {
        message: errors.token,
      });
    } catch (err) {
      console.log(err);
    }
  };

  return (
    <ModalLayout>
      <h2 className="block mt-6 text-center text-3xl font-extrabold text-chrome-900">Sign into Kubernetes</h2>
      <div className="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
        <div className="bg-background py-8 px-4 shadow-sm sm:rounded-lg sm:px-10 relative">
          <Form {...form}>
            <form className="space-y-6" onSubmit={form.handleSubmit(onSubmit)}>
              <FormField
                control={form.control}
                name="token"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Token</FormLabel>
                    <FormControl>
                      <Input placeholder="Enter your kubernetes token..." {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit">Sign in</Button>
            </form>
          </Form>
          {serverLoading && (
            <div className="absolute top-0 left-0 w-full h-full bg-background bg-opacity-80 flex justify-center items-center">
              <div className="bg-background py-4 px-6 border border-chrome-200 flex">
                <Spinner size="sm" />
                <span>Loading...</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </ModalLayout>
  );
}

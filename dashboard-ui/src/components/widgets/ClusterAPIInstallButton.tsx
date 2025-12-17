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

import { useMutation } from '@apollo/client/react';
import { CirclePlus } from 'lucide-react';
import { useState } from 'react';
import toast from 'react-hot-toast';

import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';

import * as dashboardOps from '@/lib/graphql/dashboard/ops';

type InstallButtonProps = {
  kubeContext: string;
};

const InstallButton = ({ kubeContext }: InstallButtonProps) => {
  const [install, installMutation] = useMutation(dashboardOps.HELM_INSTALL_LATEST);
  const [clicked, setClicked] = useState(false);

  const handleClick = async () => {
    setClicked(true);

    try {
      await install({ variables: { kubeContext } });
    } catch (e: unknown) {
      if (e instanceof Error) {
        toast(e.message);
      } else {
        console.error(e);
        toast('An unknown error occurred (see console)');
      }
    }
  };

  const label = clicked && installMutation.loading === false ? 'Waiting' : 'Install';

  return (
    <Button variant="outline" size="sm" onClick={handleClick} disabled={clicked}>
      {clicked ? <Spinner size="xs" /> : <CirclePlus className="h-5 w-5 mr-1" />}
      {label}
    </Button>
  );
};

export default InstallButton;

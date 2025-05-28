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

import { useSubscription } from "@apollo/client";
import { Fragment, useEffect, useState } from "react";
import {
  RecoilRoot,
  atom,
  useRecoilValue,
  useSetRecoilState,
  type SetterOrUpdater,
} from "recoil";

import appConfig from "@/app-config";
import * as dashboardOps from "@/lib/graphql/dashboard/ops";

import Form from "@kubetail/ui/elements/Form";
import { useIsClusterAPIEnabled } from "@/lib/hooks";

type EnvironmentControlWidgetProps = {
  className?: string;
};
const EnvironmentControl = ({ className }: EnvironmentControlWidgetProps) => {
  const isClusterAPIEnabled = useIsClusterAPIEnabled(null);
  const [env, setEnv] = useState<string>(() => {
    return localStorage.getItem("clusterAPIEnabled") || "";
  });

  const handleChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    console.log(isClusterAPIEnabled);
    const newEnv = e.target.value;
    setEnv(newEnv);

    localStorage.setItem("clusterAPIEnabled", newEnv);

    window.location.reload();
  };

  return (
    <div>
      <div>Environment control</div>
      <div className="pt-3 ">
        <Form.Select
          className="mt-0 w-[110px] py-0 pl-0 pr-0 h-auto border-0 focus:ring-offset-0 focus:ring-0 focus:border-transparent focus:ring-transparent text-xs bg-transparent"
          value={env}
          onChange={(ev) => {
            handleChange(ev);
          }}
        >
          <Form.Option value="enabled">Kubetail api</Form.Option>
          <Form.Option value="disabled">Kubernetes api</Form.Option>
        </Form.Select>
      </div>
    </div>
  );
};

const EnvironmentControlWidgetWrapper = (
  props: EnvironmentControlWidgetProps
) => (
  <RecoilRoot>
    <EnvironmentControl {...props} />
  </RecoilRoot>
);

export default EnvironmentControlWidgetWrapper;

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

import DataTable from "@kubetail/ui/elements/DataTable";

import appConfig from "@/app-config";
import Modal from "@/components/elements/Modal";
import ClusterAPIInstallButton from "@/components/widgets/ClusterAPIInstallButton";
import * as dashboardOps from "@/lib/graphql/dashboard/ops";
import {
  ServerStatus,
  Status,
  useDashboardServerStatus,
  useKubernetesAPIServerStatus,
  useClusterAPIServerStatus,
} from "@/lib/server-status";
import { cn } from "@/lib/util";
import Form from "@kubetail/ui/elements/Form";

type ServerStatusWidgetProps = {
  className?: string;
};
const EnvironmentControl = ({ className }: ServerStatusWidgetProps) => {
  const [env, setEnv] = useState<string>("kubetail api");
  return (
    <div className="p-3">
      <Form.Select
        className="mt-0 py-0 pl-0 pr-0 h-auto border-0 focus:ring-offset-0 focus:ring-0 focus:border-transparent focus:ring-transparent text-xs bg-transparent"
        value={env}
        onChange={(ev) => {
          setEnv(ev.target.value);
        }}
      >
        <Form.Option value="kubetail api">Kubetail api</Form.Option>
        <Form.Option value="kubernetes api">Kubernetes api</Form.Option>
      </Form.Select>
    </div>
  );
};

const EnvironmentControlWidgetWrapper = (props: ServerStatusWidgetProps) => (
  <RecoilRoot>
    <EnvironmentControl {...props} />
  </RecoilRoot>
);

export default EnvironmentControlWidgetWrapper;

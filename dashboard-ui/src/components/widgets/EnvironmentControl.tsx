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
import Form from "@kubetail/ui/elements/Form";
import { useIsClusterAPIEnabled } from "@/lib/hooks";
import Modal from "../elements/Modal";

type EnvironmentControlWidgetProps = {
  className?: string;
};

const EnvironmentControl = ({ className }: EnvironmentControlWidgetProps) => {
  const [isDialogOpen, setIsDialogOpen] = useState(false);

  const [env, setEnv] = useState<string>(() => {
    return localStorage.getItem("clusterAPIEnabled") || "";
  });
  const isClusterAPIEnabled = useIsClusterAPIEnabled(env);

  /*const handleChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    console.log(isClusterAPIEnabled);
    const newEnv = e.target.value;
    setEnv(newEnv);

    localStorage.setItem("clusterAPIEnabled", newEnv);
  };*/

  const handleChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newEnv = e.target.value;
    setEnv(newEnv);
  };

  useEffect(() => {
    console.log("EnvironmentControl useEffect", env, isClusterAPIEnabled);
    localStorage.setItem("clusterAPIEnabled", env);
  }, [env]);

  return (
    <div>
      <button
        className={`text-xs text-chrome-500 hover:text-chrome-700 pr-3 ${className}`}
        onClick={() => setIsDialogOpen(true)}
      >
        Environment Control
      </button>
      <Modal
        open={isDialogOpen}
        onClose={() => setIsDialogOpen(false)}
        className="!max-w-[550px]"
      >
        <Modal.Title className="flex items-center space-x-3">
          <span>Environment control</span>
        </Modal.Title>
        <div className="mt-5 pb-8">
          <Form.Group>
            <Form.Label>
              Switch between Kubernetes API and Kubetail API
            </Form.Label>
            <div className="pt-3 ">
              <Form.Select
                className={className}
                value={env}
                onChange={(ev) => {
                  handleChange(ev);
                }}
              >
                <Form.Option value="enabled">Kubetail api</Form.Option>
                <Form.Option value="disabled">Kubernetes api</Form.Option>
              </Form.Select>
            </div>
          </Form.Group>
        </div>
      </Modal>
    </div>
  );
};

const EnvironmentControlWidgetWrapper = (props: EnvironmentControlWidgetProps) => (
  <RecoilRoot>
    <EnvironmentControl {...props} />
  </RecoilRoot>
);

export default EnvironmentControlWidgetWrapper;

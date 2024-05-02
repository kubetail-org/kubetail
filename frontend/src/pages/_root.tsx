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

import { XCircleIcon } from '@heroicons/react/24/outline';
import { useEffect, useState } from 'react';
import toastlib, { useToaster, resolveValue } from 'react-hot-toast';
import type { Toast } from 'react-hot-toast';
import { Outlet } from 'react-router-dom';

import Button from '@kubetail/ui/elements/Button';

import Modal from '@/components/elements/Modal';
import { joinPaths, getBasename } from '@/lib/helpers';

const QueryError = ({ toast }: { toast: Toast }) => (
  <div className="relative bg-red-100 border-2 border-red-200 p-1">
    <button
      type="button"
      className="absolute top-[-10px] left-[-10px] bg-red-100 rounded-full cursor-pointer"
      onClick={() => toastlib.remove(toast.id)}
      aria-label="remove"
    >
      <XCircleIcon
        className="h-5 w-5 text-red-300 hover:text-red-400"
        aria-hidden="true"
      />
    </button>
    {resolveValue(toast.message, toast)}
  </div>
);

const CustomToaster = () => {
  const { toasts } = useToaster();
  const [modalIsOpen, setModalIsOpen] = useState(false);

  if (!toasts.length) return null;

  const handleRemoveAllClick = () => {
    toastlib.remove();
  };

  return (
    <div className="fixed bottom-[28px] right-[5px] p-2 bg-red-100 border-2 border-red-200">
      <button
        type="button"
        className="absolute top-[-10px] left-[-10px] bg-red-100 rounded-full cursor-pointer"
        onClick={handleRemoveAllClick}
        aria-label="remove all"
      >
        <XCircleIcon
          className="h-5 w-5 text-red-300 hover:text-red-400"
          aria-hidden="true"
        />
      </button>
      <button
        type="button"
        onClick={() => setModalIsOpen(true)}
      >
        Query Errors
        {`(${toasts.length})`}
      </button>
      <Modal
        className="max-w-[500px]"
        open={modalIsOpen}
        onClose={() => setModalIsOpen(false)}
      >
        <Modal.Title>
          <div className="flex justify-between">
            <span>
              Query Errors
              {`(${toasts.length})`}
            </span>
            <Button intent="danger" size="xs" onClick={() => toastlib.remove()}>Dismiss All</Button>
          </div>
        </Modal.Title>
        <div className="text-sm space-y-4">
          {toasts.map((toast) => <QueryError toast={toast} />)}
        </div>
      </Modal>
    </div>
  );
};

export default function Root() {
  // update favicon location
  useEffect(() => {
    const el = document.querySelector('link[rel="icon"]');
    if (!el) return;
    el.setAttribute('href', joinPaths(getBasename(), '/favicon.ico'));
  }, []);

  return (
    <>
      <CustomToaster />
      <Outlet />
    </>
  );
}

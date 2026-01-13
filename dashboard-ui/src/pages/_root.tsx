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

import { CircleX } from 'lucide-react';
import { useEffect, useState } from 'react';
import toastlib, { useToaster, resolveValue } from 'react-hot-toast';
import type { Toast } from 'react-hot-toast';
import { Outlet } from 'react-router-dom';

import { Button } from '@kubetail/ui/elements/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@kubetail/ui/elements/dialog';

import { cn, joinPaths, getBasename } from '@/lib/util';

const closeIconCommonClasses = 'text-red-300 hover:text-red-400 dark:text-red-700 dark:hover:text-red-600';
const closeBtnCommonClasses = 'bg-red-100 dark:bg-red-900';
const containerCommonClasses = 'bg-red-100 dark:bg-red-900 border-2 border-red-200 dark:border-red-800';

const QueryError = ({ toast }: { toast: Toast }) => (
  <div className={cn('relative min-w-0 break-all p-1', containerCommonClasses)}>
    <button
      type="button"
      className={cn('absolute top-[-10px] left-[-10px] rounded-full', closeBtnCommonClasses)}
      onClick={() => toastlib.remove(toast.id)}
      aria-label="remove"
    >
      <CircleX className={cn('h-5 w-5', closeIconCommonClasses)} aria-hidden="true" />
    </button>
    <div className="max-w-full overflow-ellipsis">{resolveValue(toast.message, toast)}</div>
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
    <>
      <div className={cn('fixed bottom-[28px] right-[5px] p-2 z-10', containerCommonClasses)}>
        <button
          type="button"
          className={cn('absolute top-[-10px] left-[-10px] rounded-full', closeBtnCommonClasses)}
          onClick={handleRemoveAllClick}
          aria-label="remove all"
        >
          <CircleX className={cn('h-5 w-5', closeIconCommonClasses)} aria-hidden="true" />
        </button>
        <button type="button" onClick={() => setModalIsOpen(true)}>
          Query Errors
          {`(${toasts.length})`}
        </button>
      </div>
      <Dialog open={modalIsOpen} onOpenChange={setModalIsOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex justify-between items-center">
              <span>
                Query Errors
                {`(${toasts.length})`}
              </span>
              <Button className="mr-4" variant="destructive" size="sm" onClick={() => toastlib.remove()}>
                Dismiss All
              </Button>
            </DialogTitle>
          </DialogHeader>
          <DialogDescription />
          <div className="text-sm space-y-4 min-w-0">
            {toasts.map((toast) => (
              <QueryError key={toast.id} toast={toast} />
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </>
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
      <Outlet />
      <CustomToaster />
    </>
  );
}

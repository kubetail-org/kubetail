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

import { Dialog, DialogProps, DialogTitleProps } from '@headlessui/react';
import React, { useRef } from 'react';

import { cn } from '@/lib/util';

type ModalProps<T extends React.ElementType> = DialogProps<T> & {
  className?: string;
};

type ModalTitleProps<T extends React.ElementType> = DialogTitleProps<T> & {
  className?: string;
};

const modalTitleBaseCls = 'text-xl mb-6';
const modalPanelBaseCls = 'relative bg-background rounded-lg overflow-hidden shadow-xl my-8 max-w-sm w-full p-6';

const Modal = React.forwardRef(
  ({ children, className, ...props }: ModalProps<'div'>, ref: React.ForwardedRef<HTMLDivElement>) => {
    const focusRef = useRef<HTMLDivElement>(null);

    return (
      <Dialog {...props} ref={ref} initialFocus={focusRef} className="relative z-10">
        {/* backdrop */}
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" ref={focusRef} />

        {/* full-screen container to center the panel */}
        <div className="fixed z-10 inset-0 overflow-y-auto">
          <div className="flex items-center justify-center min-h-full">
            <Dialog.Panel className={cn(modalPanelBaseCls, className)}>{children}</Dialog.Panel>
          </div>
        </div>
      </Dialog>
    );
  },
);

Modal.displayName = 'Modal';

const ModalTitle = React.forwardRef(
  ({ children, className, ...props }: ModalTitleProps<'h2'>, ref: React.ForwardedRef<HTMLHeadingElement>) => (
    <Dialog.Title {...props} ref={ref} className={cn(modalTitleBaseCls, className)}>
      {children}
    </Dialog.Title>
  ),
);

ModalTitle.displayName = 'ModalTitle';

// add name to default export for linting rule: react-refresh/only-export-components
const DefaultExport = Object.assign(Modal, {
  Title: ModalTitle,
});

DefaultExport.displayName = 'Modal';

export default DefaultExport;

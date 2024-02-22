import { useEffect, useState } from 'react';

import Form from 'kubetail-ui/elements/Form';

import ServerStatus from '@/components/widgets/ServerStatus';

// TODO: this should get combined with the code in _root.tsx
export default function ({ children }: React.PropsWithChildren) {
  const [themeDropdownValue, setThemeDropdownValue] = useState(() => {
    if (!('theme' in localStorage)) return 'system';
    if (localStorage.theme === 'dark') return 'user-dark';
    return 'user-light';
  });

  useEffect(() => {
    let theme = 'light';
    if (themeDropdownValue === 'user-dark' || (themeDropdownValue === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
      theme = 'dark';
    }
    if (theme === 'dark') document.documentElement.classList.add('dark');
    else document.documentElement.classList.remove('dark');
  }, [themeDropdownValue]);

  const handleThemeDropdownChange = (ev: React.ChangeEvent<HTMLSelectElement>) => {
    switch (ev.target.value) {
      case 'system':
        localStorage.removeItem('theme');
        setThemeDropdownValue('system');
        break;
      case 'user-dark':
        localStorage.setItem('theme', 'dark');
        setThemeDropdownValue('user-dark');
        break;
      case 'user-light':
        localStorage.setItem('theme', 'light');
        setThemeDropdownValue('user-light');
        break;
      default:
        throw new Error('not implemented');
    }
  };

  useEffect(() => {
    if (themeDropdownValue === 'user-dark') {
      localStorage.setItem('theme', 'dark');
    }
  }, [themeDropdownValue])

  return (
    <>
      <div className="h-[calc(100vh-23px)] overflow-auto">
        {children}
      </div>
      <div className="h-[22px] bg-chrome-100 border-t border-chrome-divider text-sm flex justify-between items-center pl-[2px]">
        <div className="flex space-x-1 text-xs">
          <div>theme: </div>
          <Form.Select
            className="w-[70px] mt-0 py-0 pl-1 pr-0 h-auto border-0 focus:ring-offset-0 focus:ring-0 focus:border-transparent focus:ring-transparent text-xs"
            value={themeDropdownValue}
            onChange={handleThemeDropdownChange}
          >
            <Form.Option value="system">system</Form.Option>
            <Form.Option value="user-dark">dark</Form.Option>
            <Form.Option value="user-light">light</Form.Option>
          </Form.Select>

        </div>
        <ServerStatus />
      </div>
    </>
  );
}

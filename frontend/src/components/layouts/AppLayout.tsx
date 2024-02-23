import Form from 'kubetail-ui/elements/Form';

import ServerStatus from '@/components/widgets/ServerStatus';
import { useTheme, UserPreference } from '@/lib/theme';

export default function ({ children }: React.PropsWithChildren) {
  const { userPreference, setUserPreference } = useTheme();

  const handleChange = (ev: React.ChangeEvent<HTMLSelectElement>) => {
    setUserPreference(ev.target.value as UserPreference);
  };

  return (
    <>
      <div className="h-[calc(100vh-23px)] overflow-auto">
        {children}
      </div>
      <div className="h-[22px] bg-chrome-100 border-t border-chrome-divider text-sm flex justify-between items-center pl-[10px]">
        <Form.Select
          className="w-[65px] mt-0 py-0 pl-0 pr-0 h-auto border-0 focus:ring-offset-0 focus:ring-0 focus:border-transparent focus:ring-transparent text-xs bg-transparent"
          value={userPreference}
          onChange={handleChange}
        >
          <Form.Option value={UserPreference.System}>system</Form.Option>
          <Form.Option value={UserPreference.Dark}>dark</Form.Option>
          <Form.Option value={UserPreference.Light}>light</Form.Option>
        </Form.Select>
        <ServerStatus />
      </div>
    </>
  );
}

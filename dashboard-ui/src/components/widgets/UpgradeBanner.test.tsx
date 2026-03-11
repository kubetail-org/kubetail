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

import { fireEvent, render, screen } from '@testing-library/react';

import UpgradeBanner from './UpgradeBanner';

describe('UpgradeBanner', () => {
  it('renders CLI update message', () => {
    render(<UpgradeBanner currentVersion="0.9.0" latestVersion="1.0.0" dismiss={vi.fn()} dontRemindMe={vi.fn()} />);

    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.getByText(/CLI 0\.9\.0/)).toBeInTheDocument();
    expect(screen.getByText(/1\.0\.0/)).toBeInTheDocument();
  });

  it('calls dismiss when dismiss button is clicked', () => {
    const dismiss = vi.fn();

    render(<UpgradeBanner currentVersion="0.9.0" latestVersion="1.0.0" dismiss={dismiss} dontRemindMe={vi.fn()} />);

    fireEvent.click(screen.getByLabelText('Dismiss'));
    expect(dismiss).toHaveBeenCalledOnce();
  });

  it('calls dontRemindMe when "Don\'t remind me" is clicked', () => {
    const dontRemindMe = vi.fn();

    render(
      <UpgradeBanner currentVersion="0.9.0" latestVersion="1.0.0" dismiss={vi.fn()} dontRemindMe={dontRemindMe} />,
    );

    fireEvent.click(screen.getByText("Don't remind me"));
    expect(dontRemindMe).toHaveBeenCalledOnce();
  });
});

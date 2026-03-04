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

import { useCallback, useState } from 'react';

import Footer from '@/components/widgets/Footer';
import UpgradeBanner from '@/components/widgets/UpgradeBanner';

export default function AppLayout({ children }: React.PropsWithChildren) {
  const [bannerHeight, setBannerHeight] = useState(0);

  const bannerRef = useCallback((node: HTMLDivElement | null) => {
    if (!node) {
      setBannerHeight(0);
      return;
    }
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) setBannerHeight(entry.contentRect.height);
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  return (
    <>
      <div ref={bannerRef}>
        <UpgradeBanner />
      </div>
      <div className="overflow-hidden" style={{ height: `calc(100vh - 23px - ${bannerHeight}px)` }}>
        {children}
      </div>
      <Footer />
    </>
  );
}

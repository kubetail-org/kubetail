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

import { useEffect, useState } from 'react';

import TimeAgo from 'react-timeago';

const useAdaptiveMinPeriod = (date: Date) => {
  const [minPeriod, setMinPeriod] = useState(10);

  useEffect(() => {
    const updateMinPeriod = () => {
      const ageMs = Date.now() - date.getTime();

      const ageMinutes = ageMs / (1000 * 60);
      if (ageMinutes < 1) {
        setMinPeriod(1); // 1 seconds for first minute
        return 60 * 1000 - ageMs;
      }

      const ageHours = ageMinutes / 60;
      if (ageHours < 1) {
        setMinPeriod(60); // 1 minute until 1 hour
        return 60 * 60 * 1000 - ageMs;
      }

      setMinPeriod(3600); // 1 hour after that
      return Infinity;
    };

    const checkMs = updateMinPeriod();

    // Schedule next check, if necessary
    if (checkMs !== Infinity) {
      const interval = setInterval(updateMinPeriod, checkMs);
      return () => clearInterval(interval);
    }
  }, [date]);

  return minPeriod;
};

const AdaptiveTimeAgo = ({ date }: { date: Date }) => {
  const minPeriod = useAdaptiveMinPeriod(date);
  return <TimeAgo date={date} minPeriod={minPeriod} title={date.toUTCString()} />;
};

export default AdaptiveTimeAgo;

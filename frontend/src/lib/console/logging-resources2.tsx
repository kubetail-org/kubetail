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

import { createContext, useContext } from 'react';

type Context = {};

const Context = createContext<Context>({} as Context);

/**
 * Log feed hook
 */

export function useLogFeed() {
  return {
    play: () => console.log('play'),
    pause: () => console.log('pause'),
    skipForward: () => console.log('skipForward'),
    query: () => console.log('query'),
  };
}

/**
 * Log feed content component
 */

export const LogFeedContent = () => {
  return (
    <div>hi</div>
  );
};

/**
 * Provider component
 */

interface LoggingResourcesProviderProps extends React.PropsWithChildren {
  sourcePaths: string[];
};

export const LoggingResourcesProvider = ({ sourcePaths, children }: LoggingResourcesProviderProps) => {
  return (
    <Context.Provider value={{}}>
      {children}
    </Context.Provider>
  );
};

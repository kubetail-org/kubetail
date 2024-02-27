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

import React, { createContext, useEffect, useReducer } from 'react';

import { LogFeedState } from './hooks';

type State = {
  logFeedState: LogFeedState;
  records: [number, number, number, number, number, number, number][];
};

type Context = {
  state: State;
  dispatch: React.Dispatch<Partial<State>>;
};

export const Context = createContext<Context>({} as Context);

function reducer(prevState: State, newState: Partial<State>): State {
  return Object.assign({}, { ...prevState }, { ...newState });
}

/**
 * Provider component
 */

interface LoggingResourcesProviderProps extends React.PropsWithChildren {
  sourcePaths: string[];
};

export const LoggingResourcesProvider = ({ sourcePaths, children }: LoggingResourcesProviderProps) => {
  const [state, dispatch] = useReducer(reducer, {
    logFeedState: LogFeedState.Streaming,
    records: [],
  });

  useEffect(() => {
    if (state.logFeedState === LogFeedState.Streaming) {
      const id = setInterval(() => {
        const records = state.records;
        records.push([
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
        ]);
        dispatch({ records });
      }, 1000);
      return () => clearInterval(id);
    }
  }, [state.logFeedState]);

  return (
    <Context.Provider value={{ state, dispatch }}>
      {children}
    </Context.Provider>
  );
};

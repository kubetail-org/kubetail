import { createContext, useContext, useEffect, useMemo, useState } from 'react';

type Context = {
  freqChangingVar: number;
  setFreqChangingVar: React.Dispatch<number>;
  slowChangingVar: number;
  setSlowChangingVar: React.Dispatch<number>;
};

const Context = createContext({} as Context);

const useContextSelective = (key: string) => {
  const contextValue = useContext(Context);
  const [value, setValue] = useState(contextValue.slowChangingVar);

  useEffect(() => {
    setValue(contextValue.slowChangingVar);
  }, [contextValue.slowChangingVar])

  return value;
}

const DisplayFreq = () => {
  console.log('<DisplayFreq />');
  const { freqChangingVar } = useContext(Context);
  return <div>freq: {freqChangingVar}</div>
};

const DisplaySlow = () => {
  console.log('<DisplaySlow />');
  //const { slowChangingVar } = useContext(Context);
  const slowChangingVar = useContextSelective('slowChangingVar');
  return <div>slow: {slowChangingVar}</div>
};

export default function Page() {
  const [freqChangingVar, setFreqChangingVar] = useState(Math.random());
  const [slowChangingVar, setSlowChangingVar] = useState(Math.random());

  const memoizedSlowChangingVar = useMemo(() => slowChangingVar, [slowChangingVar]);

  useEffect(() => {
    const id1 = setInterval(() => {
      setFreqChangingVar(Math.random());
    }, 500);

    const id2 = setInterval(() => {
      setSlowChangingVar(Math.random());
    }, 3000);

    return () => {
      clearInterval(id1);
      clearInterval(id2);
    };
  }, []);

  return (
    <Context.Provider value={{
      freqChangingVar,
      setFreqChangingVar,
      slowChangingVar: memoizedSlowChangingVar,
      setSlowChangingVar,
    }}>
      <DisplayFreq />
      <DisplaySlow />
    </Context.Provider>
  );
}

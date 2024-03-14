import { useEffect } from 'react';
import { RecoilRoot, atom, useRecoilState, useRecoilValue } from 'recoil';

const val1State = atom({
  key: 'val1State', // unique ID (with respect to other atoms/selectors)
  default: Math.random(), // default value (aka initial value)
});

const val2State = atom({
  key: 'val2State', // unique ID (with respect to other atoms/selectors)
  default: Math.random(), // default value (aka initial value)
});

function ValUpdater() {
  const [, setVal1] = useRecoilState(val1State);
  const [, setVal2] = useRecoilState(val2State);

  useEffect(() => {
    const id1 = setInterval(() => {
      setVal1(Math.random());
    }, 500);

    const id2 = setInterval(() => {
      setVal2(Math.random());
    }, 3000);

    return () => {
      clearInterval(id1);
      clearInterval(id2);
    };
  }, []);

  return <></>;
}

function DisplayVal1() {
  const val1 = useRecoilValue(val1State);
  return <div>{val1}</div>;
}

function DisplayVal2() {
  const val2 = useRecoilValue(val2State);
  return <div>{val2}</div>;
}

export default function Page() {
  return (
    <RecoilRoot>
      <DisplayVal1 />
      <DisplayVal2 />
      <ValUpdater />
    </RecoilRoot>
  );
}

/*
import { RecoilRoot, atom, selector, useRecoilState, useRecoilValue } from 'recoil';

const textState = atom({
  key: 'textState', // unique ID (with respect to other atoms/selectors)
  default: '', // default value (aka initial value)
});

const charCountState = selector({
  key: 'charCountState', // unique ID (with respect to other atoms/selectors)
  get: ({get}) => {
    const text = get(textState);

    return text.length;
  },
});

function CharacterCount() {
  const count = useRecoilValue(charCountState);

  return <>Character Count: {count}</>;
}

function CharacterCounter() {
  return (
    <div>
      <TextInput />
      <CharacterCount />
    </div>
  );
}

function TextInput() {
  const [text, setText] = useRecoilState(textState);

  const onChange = (event) => {
    setText(event.target.value);
  };

  return (
    <div>
      <input type="text" value={text} onChange={onChange} />
      <br />
      Echo: {text}
    </div>
  );
}

export default function Page() {
  return (
    <RecoilRoot>
      <CharacterCounter />
    </RecoilRoot>
  );
}
*/

/*
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
*/

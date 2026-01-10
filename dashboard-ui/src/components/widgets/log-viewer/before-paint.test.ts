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

import { renderHook } from '@testing-library/react';

import { useBeforePaint } from './before-paint';

describe('useBeforePaint', () => {
  it('should return a stable callback function', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const firstCallback = result.current;
    rerender({ trigger: 1 });
    const secondCallback = result.current;

    expect(firstCallback).toBe(secondCallback);
  });

  it('should execute a single queued callback', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const mockCallback = vi.fn();
    result.current(mockCallback);

    expect(mockCallback).not.toHaveBeenCalled();

    // Trigger the effect by changing the trigger
    rerender({ trigger: 1 });

    expect(mockCallback).toHaveBeenCalledTimes(1);
  });

  it('should execute multiple queued callbacks in order', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const callOrder: number[] = [];
    const mockCallback1 = vi.fn(() => {
      callOrder.push(1);
    });
    const mockCallback2 = vi.fn(() => {
      callOrder.push(2);
    });
    const mockCallback3 = vi.fn(() => {
      callOrder.push(3);
    });

    result.current(mockCallback1);
    result.current(mockCallback2);
    result.current(mockCallback3);

    expect(mockCallback1).not.toHaveBeenCalled();
    expect(mockCallback2).not.toHaveBeenCalled();
    expect(mockCallback3).not.toHaveBeenCalled();

    rerender({ trigger: 1 });

    expect(mockCallback1).toHaveBeenCalledTimes(1);
    expect(mockCallback2).toHaveBeenCalledTimes(1);
    expect(mockCallback3).toHaveBeenCalledTimes(1);
    expect(callOrder).toEqual([1, 2, 3]);
  });

  it('should clear the queue after executing callbacks', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const mockCallback = vi.fn();
    result.current(mockCallback);

    rerender({ trigger: 1 });

    expect(mockCallback).toHaveBeenCalledTimes(1);

    // Trigger again without adding new callbacks
    rerender({ trigger: 2 });

    // Should still be called only once
    expect(mockCallback).toHaveBeenCalledTimes(1);
  });

  it('should not execute callbacks if trigger does not change', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const mockCallback = vi.fn();
    result.current(mockCallback);

    rerender({ trigger: 0 });

    expect(mockCallback).not.toHaveBeenCalled();
  });

  it('should not execute callbacks if queue is empty when trigger changes', () => {
    const { rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    // Change trigger without adding any callbacks
    rerender({ trigger: 1 });

    // No error should occur, and nothing should execute
    expect(true).toBe(true);
  });

  it('should handle new callbacks added after previous execution', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const mockCallback1 = vi.fn();
    result.current(mockCallback1);

    rerender({ trigger: 1 });

    expect(mockCallback1).toHaveBeenCalledTimes(1);

    // Add a new callback and trigger again
    const mockCallback2 = vi.fn();
    result.current(mockCallback2);

    rerender({ trigger: 2 });

    expect(mockCallback1).toHaveBeenCalledTimes(1); // Should not be called again
    expect(mockCallback2).toHaveBeenCalledTimes(1);
  });

  it('should work with different trigger types', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: { id: number } }) => useBeforePaint(trigger), {
      initialProps: { trigger: { id: 1 } },
    });

    const mockCallback = vi.fn();
    result.current(mockCallback);

    rerender({ trigger: { id: 2 } });

    expect(mockCallback).toHaveBeenCalledTimes(1);
  });

  it('should execute callbacks added during the same render cycle', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const mockCallback1 = vi.fn();
    const mockCallback2 = vi.fn();

    // Add multiple callbacks before triggering
    result.current(mockCallback1);
    result.current(mockCallback2);

    rerender({ trigger: 1 });

    expect(mockCallback1).toHaveBeenCalledTimes(1);
    expect(mockCallback2).toHaveBeenCalledTimes(1);
  });

  it('should execute all callbacks even if queue is large', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const callbacks = Array.from({ length: 100 }, () => vi.fn());

    callbacks.forEach((cb) => result.current(cb));

    rerender({ trigger: 1 });

    callbacks.forEach((cb) => {
      expect(cb).toHaveBeenCalledTimes(1);
    });
  });

  it('should allow callbacks to be queued immediately after mounting', () => {
    const { result, rerender } = renderHook(({ trigger }: { trigger: number }) => useBeforePaint(trigger), {
      initialProps: { trigger: 0 },
    });

    const mockCallback = vi.fn();

    // Queue immediately after mount
    result.current(mockCallback);

    // Trigger the effect
    rerender({ trigger: 1 });

    expect(mockCallback).toHaveBeenCalledTimes(1);
  });
});

// Copyright 2024-2026 The Kubetail Authors
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

import { DoubleTailedArray } from './double-tailed-array';

describe('DoubleTailedArray', () => {
  describe('constructor', () => {
    it('should create an empty array when no items provided', () => {
      const arr = new DoubleTailedArray<number>();
      expect(arr.length).toBe(0);
      expect(arr.toArray()).toEqual([]);
    });

    it('should create array with initial items', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      expect(arr.length).toBe(3);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should create array with empty initial array', () => {
      const arr = new DoubleTailedArray<number>([]);
      expect(arr.length).toBe(0);
      expect(arr.toArray()).toEqual([]);
    });
  });

  describe('length', () => {
    it('should return 0 for empty array', () => {
      const arr = new DoubleTailedArray<number>();
      expect(arr.length).toBe(0);
    });

    it('should return correct length after appends', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([1]);
      expect(arr.length).toBe(1);
      arr.append([2]);
      expect(arr.length).toBe(2);
    });

    it('should return correct length after prepends', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([1]);
      expect(arr.length).toBe(1);
      arr.prepend([2]);
      expect(arr.length).toBe(2);
    });

    it('should return correct length with mixed operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([1]);
      arr.prepend([2]);
      arr.append([3]);
      arr.prepend([4]);
      expect(arr.length).toBe(4);
    });
  });

  describe('append', () => {
    it('should add element to the end', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([1]);
      arr.append([2]);
      arr.append([3]);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should add element to end of prepended elements', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([1]);
      arr.append([2]);
      expect(arr.toArray()).toEqual([1, 2]);
    });

    it('should handle different types', () => {
      const arr = new DoubleTailedArray<string>();
      arr.append(['hello']);
      arr.append(['world']);
      expect(arr.toArray()).toEqual(['hello', 'world']);
    });

    it('should add multiple elements at once', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([1, 2, 3]);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should handle variadic append with existing elements', () => {
      const arr = new DoubleTailedArray<number>([1, 2]);
      arr.append([3, 4, 5]);
      expect(arr.toArray()).toEqual([1, 2, 3, 4, 5]);
    });

    it('should handle empty variadic arguments', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([1]);
      expect(arr.toArray()).toEqual([1]);
    });
  });

  describe('prepend', () => {
    it('should add element to the beginning', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([3]);
      arr.prepend([2]);
      arr.prepend([1]);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should add element to beginning of appended elements', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([2]);
      arr.prepend([1]);
      expect(arr.toArray()).toEqual([1, 2]);
    });

    it('should handle mixed prepend and append operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([3]);
      arr.prepend([1]);
      arr.append([4]);
      arr.prepend([0]);
      arr.append([5]);
      expect(arr.toArray()).toEqual([0, 1, 3, 4, 5]);
    });

    it('should add multiple elements at once', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([1, 2, 3]);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should handle variadic prepend with existing elements', () => {
      const arr = new DoubleTailedArray<number>([4, 5]);
      arr.prepend([1, 2, 3]);
      expect(arr.toArray()).toEqual([1, 2, 3, 4, 5]);
    });

    it('should handle variadic prepend in correct order', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([4]);
      arr.prepend([1, 2, 3]);
      expect(arr.toArray()).toEqual([1, 2, 3, 4]);
    });

    it('should handle empty variadic arguments', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([1]);
      expect(arr.toArray()).toEqual([1]);
    });
  });

  describe('at', () => {
    it('should return element at index', () => {
      const arr = new DoubleTailedArray<number>([10, 20, 30]);
      expect(arr.at(0)).toBe(10);
      expect(arr.at(1)).toBe(20);
      expect(arr.at(2)).toBe(30);
    });

    it('should throw for out of bounds index', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      expect(() => arr.at(-1)).toThrow('Index -1 is out of bounds for array of length 3');
      expect(() => arr.at(3)).toThrow('Index 3 is out of bounds for array of length 3');
      expect(() => arr.at(100)).toThrow('Index 100 is out of bounds for array of length 3');
    });

    it('should throw for empty array', () => {
      const arr = new DoubleTailedArray<number>();
      expect(() => arr.at(0)).toThrow('Index 0 is out of bounds for array of length 0');
    });

    it('should work correctly with prepended elements', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([2]);
      arr.prepend([1]);
      arr.append([3]);
      expect(arr.at(0)).toBe(1);
      expect(arr.at(1)).toBe(2);
      expect(arr.at(2)).toBe(3);
    });

    it('should access elements in before array correctly', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([3]);
      arr.prepend([2]);
      arr.prepend([1]);
      expect(arr.at(0)).toBe(1);
      expect(arr.at(1)).toBe(2);
      expect(arr.at(2)).toBe(3);
    });

    it('should access elements in after array correctly', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([1]);
      arr.append([2]);
      arr.append([3]);
      expect(arr.at(0)).toBe(1);
      expect(arr.at(1)).toBe(2);
      expect(arr.at(2)).toBe(3);
    });
  });

  describe('set', () => {
    it('should set element at index and return true', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      expect(arr.set(1, 20)).toBe(true);
      expect(arr.toArray()).toEqual([1, 20, 3]);
    });

    it('should return false for out of bounds index', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      expect(arr.set(-1, 10)).toBe(false);
      expect(arr.set(3, 10)).toBe(false);
      expect(arr.set(100, 10)).toBe(false);
    });

    it('should return false for empty array', () => {
      const arr = new DoubleTailedArray<number>();
      expect(arr.set(0, 10)).toBe(false);
    });

    it('should set elements in before array correctly', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([2]);
      arr.prepend([1]);
      arr.append([3]);
      arr.set(0, 10);
      arr.set(1, 20);
      expect(arr.toArray()).toEqual([10, 20, 3]);
    });

    it('should set elements in after array correctly', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([1]);
      arr.append([2]);
      arr.append([3]);
      arr.set(1, 20);
      arr.set(2, 30);
      expect(arr.toArray()).toEqual([1, 20, 30]);
    });

    it('should handle mixed before and after modifications', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([3]);
      arr.prepend([1]);
      arr.append([4]);
      arr.prepend([0]);
      // Array is now [0, 1, 3, 4]
      arr.set(0, 100);
      arr.set(3, 400);
      expect(arr.toArray()).toEqual([100, 1, 3, 400]);
    });
  });

  describe('toArray', () => {
    it('should return empty array when empty', () => {
      const arr = new DoubleTailedArray<number>();
      expect(arr.toArray()).toEqual([]);
    });

    it('should return correct order with only appends', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([1]);
      arr.append([2]);
      arr.append([3]);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should return correct order with only prepends', () => {
      const arr = new DoubleTailedArray<number>();
      arr.prepend([3]);
      arr.prepend([2]);
      arr.prepend([1]);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });

    it('should return correct order with mixed operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([3]);
      arr.prepend([2]);
      arr.append([4]);
      arr.prepend([1]);
      arr.append([5]);
      expect(arr.toArray()).toEqual([1, 2, 3, 4, 5]);
    });

    it('should not modify the original array', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      const result = arr.toArray();
      result.push(4);
      expect(arr.toArray()).toEqual([1, 2, 3]);
    });
  });

  describe('forEach', () => {
    it('should not call callback for empty array', () => {
      const arr = new DoubleTailedArray<number>();
      const callback = vi.fn();
      arr.forEach(callback);
      expect(callback).not.toHaveBeenCalled();
    });

    it('should call callback for each element', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      const callback = vi.fn();
      arr.forEach(callback);
      expect(callback).toHaveBeenCalledTimes(3);
      expect(callback).toHaveBeenNthCalledWith(1, 1, 0, arr);
      expect(callback).toHaveBeenNthCalledWith(2, 2, 1, arr);
      expect(callback).toHaveBeenNthCalledWith(3, 3, 2, arr);
    });

    it('should work with mixed operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([2]);
      arr.prepend([1]);
      arr.append([3]);

      const result: number[] = [];
      arr.forEach((value) => result.push(value));
      expect(result).toEqual([1, 2, 3]);
    });

    it('should provide correct index', () => {
      const arr = new DoubleTailedArray<string>(['a', 'b', 'c']);
      const indices: number[] = [];
      arr.forEach((_, index) => indices.push(index));
      expect(indices).toEqual([0, 1, 2]);
    });
  });

  describe('map', () => {
    it('should return empty array for empty input', () => {
      const arr = new DoubleTailedArray<number>();
      const result = arr.map((x) => x * 2);
      expect(result).toEqual([]);
    });

    it('should map elements correctly', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      const result = arr.map((x) => x * 2);
      expect(result).toEqual([2, 4, 6]);
    });

    it('should work with type transformation', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      const result = arr.map((x) => `num${x}`);
      expect(result).toEqual(['num1', 'num2', 'num3']);
    });

    it('should work with mixed operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([3]);
      arr.prepend([1]);
      arr.append([4]);
      arr.prepend([0]);

      const result = arr.map((x) => x + 10);
      expect(result).toEqual([10, 11, 13, 14]);
    });

    it('should provide correct index and array', () => {
      const arr = new DoubleTailedArray<number>([10, 20, 30]);
      const result = arr.map((value, index, array) => ({
        value,
        index,
        length: array.length,
      }));

      expect(result).toEqual([
        { value: 10, index: 0, length: 3 },
        { value: 20, index: 1, length: 3 },
        { value: 30, index: 2, length: 3 },
      ]);
    });
  });

  describe('first', () => {
    it('should throw for empty array', () => {
      const arr = new DoubleTailedArray<number>();
      expect(() => arr.first()).toThrow('Index 0 is out of bounds for array of length 0');
    });

    it('should return first element', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      expect(arr.first()).toBe(1);
    });

    it('should return first element after prepends', () => {
      const arr = new DoubleTailedArray<number>([2, 3]);
      arr.prepend([1]);
      expect(arr.first()).toBe(1);
    });

    it('should return first element with mixed operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([5]);
      arr.prepend([3]);
      arr.prepend([1]);
      arr.append([7]);
      expect(arr.first()).toBe(1);
    });
  });

  describe('last', () => {
    it('should throw for empty array', () => {
      const arr = new DoubleTailedArray<number>();
      expect(() => arr.last()).toThrow('Index -1 is out of bounds for array of length 0');
    });

    it('should return last element', () => {
      const arr = new DoubleTailedArray<number>([1, 2, 3]);
      expect(arr.last()).toBe(3);
    });

    it('should return last element after appends', () => {
      const arr = new DoubleTailedArray<number>([1, 2]);
      arr.append([3]);
      expect(arr.last()).toBe(3);
    });

    it('should return last element with mixed operations', () => {
      const arr = new DoubleTailedArray<number>();
      arr.append([5]);
      arr.prepend([3]);
      arr.prepend([1]);
      arr.append([7]);
      expect(arr.last()).toBe(7);
    });
  });

  describe('complex scenarios', () => {
    it('should handle alternating prepend and append', () => {
      const arr = new DoubleTailedArray<number>();
      for (let i = 0; i < 5; i += 1) {
        if (i % 2 === 0) {
          arr.append([i]);
        } else {
          arr.prepend([i]);
        }
      }
      expect(arr.toArray()).toEqual([3, 1, 0, 2, 4]);
    });

    it('should maintain consistency across operations', () => {
      const arr = new DoubleTailedArray<string>(['b', 'c']);
      arr.prepend(['a']);
      arr.append(['d']);

      expect(arr.length).toBe(4);
      expect(arr.first()).toBe('a');
      expect(arr.last()).toBe('d');
      expect(arr.at(1)).toBe('b');
      expect(arr.toArray()).toEqual(['a', 'b', 'c', 'd']);

      arr.set(1, 'B');
      expect(arr.toArray()).toEqual(['a', 'B', 'c', 'd']);
    });

    it('should work with objects', () => {
      const arr = new DoubleTailedArray<{ id: number; name: string }>();
      arr.append([{ id: 1, name: 'first' }]);
      arr.append([{ id: 2, name: 'second' }]);
      arr.prepend([{ id: 0, name: 'zero' }]);

      expect(arr.length).toBe(3);
      expect(arr.first()?.name).toBe('zero');
      expect(arr.last()?.name).toBe('second');
      expect(arr.at(1)?.id).toBe(1);
    });
  });
});

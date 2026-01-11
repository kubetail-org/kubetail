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

export class OutOfBoundsError extends Error {
  constructor(index: number, length: number) {
    super(`Index ${index} is out of bounds for array of length ${length}`);
    this.name = 'OutOfBoundsError';
  }
}

/**
 * A double-tailed array data structure that supports efficient append and prepend operations.
 *
 * Internally uses two arrays:
 * - `before`: stores prepended elements in reverse order
 * - `after`: stores appended elements in normal order
 *
 * Indexing semantics match normal arrays (0-based from the logical start).
 */

export class DoubleTailedArray<T> {
  private before: T[] = []; // Stores elements in reverse order

  private after: T[] = [];

  constructor(items?: T[]) {
    if (items) {
      this.after = [...items];
    }
  }

  /**
   * Get the total number of elements in the array
   */
  get length(): number {
    return this.before.length + this.after.length;
  }

  /**
   * Access element at index (0-based from logical start)
   */
  at(index: number): T {
    if (index < 0 || index >= this.length) {
      throw new OutOfBoundsError(index, this.length);
    }

    if (index < this.before.length) {
      // Element is in the before array (stored in reverse)
      return this.before[this.before.length - 1 - index];
    }
    // Element is in the after array
    return this.after[index - this.before.length];
  }

  /**
   * Set element at index (0-based from logical start)
   */
  set(index: number, value: T): boolean {
    if (index < 0 || index >= this.length) {
      return false;
    }

    if (index < this.before.length) {
      // Element is in the before array (stored in reverse)
      this.before[this.before.length - 1 - index] = value;
    } else {
      // Element is in the after array
      this.after[index - this.before.length] = value;
    }
    return true;
  }

  /**
   * Add element(s) to the end of the array
   */
  append(values: T[]): void {
    for (let i = 0; i < values.length; i += 1) {
      this.after.push(values[i]);
    }
  }

  /**
   * Add element(s) to the beginning of the array
   * Elements are added in order, so prepend(1, 2, 3) will add [1, 2, 3] at the start
   */
  prepend(values: T[]): void {
    // Push in reverse order so they appear in correct order at the beginning
    for (let i = values.length - 1; i >= 0; i -= 1) {
      this.before.push(values[i]);
    }
  }

  /**
   * Convert to a regular array
   */
  toArray(): T[] {
    return [...this.before].reverse().concat(this.after);
  }

  /**
   * Execute a callback for each element
   */
  forEach(callback: (value: T, index: number, array: this) => void): void {
    for (let i = 0; i < this.length; i += 1) {
      callback(this.at(i), i, this);
    }
  }

  /**
   * Map elements to a new array
   */
  map<U>(callback: (value: T, index: number, array: this) => U): U[] {
    const result: U[] = [];
    for (let i = 0; i < this.length; i += 1) {
      result.push(callback(this.at(i), i, this));
    }
    return result;
  }

  /**
   * Get the first element
   */
  first(): T {
    return this.at(0);
  }

  /**
   * Get the last element
   */
  last(): T {
    return this.at(this.length - 1);
  }
}

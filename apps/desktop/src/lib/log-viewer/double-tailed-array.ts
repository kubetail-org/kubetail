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

// An array that grows at both ends in amortised O(1). The head is held reversed, so a
// prepend is a push onto it. Each element also has a key that never changes while it is
// held: keys run `keyOffset`, `keyOffset + 1`, …, a prepend lowers `keyOffset` and a
// `dropFirst` raises it.
export class DoubleTailedArray<T> {
  private head: T[] = [];

  private tail: T[] = [];

  private keyOffset = 0;

  get length(): number {
    return this.head.length + this.tail.length;
  }

  at(index: number): T {
    if (!Number.isInteger(index) || index < 0 || index >= this.length) {
      throw new RangeError(`index ${index} is outside 0..${this.length - 1}`);
    }
    return index < this.head.length ? this.head[this.head.length - 1 - index] : this.tail[index - this.head.length];
  }

  first(): T | undefined {
    return this.length === 0 ? undefined : this.at(0);
  }

  last(): T | undefined {
    return this.length === 0 ? undefined : this.at(this.length - 1);
  }

  keyAt(index: number): number {
    return this.keyOffset + index;
  }

  /** Unchecked: a key outside the held range answers an index outside the length. */
  indexOfKey(key: number): number {
    return key - this.keyOffset;
  }

  // Both add with a loop: a spread's arguments are capped, and a follow's batch is not.
  append(items: readonly T[]): void {
    for (let i = 0; i < items.length; i += 1) this.tail.push(items[i]);
  }

  prepend(items: readonly T[]): void {
    for (let i = items.length - 1; i >= 0; i -= 1) this.head.push(items[i]);
    this.keyOffset -= items.length;
  }

  /** Drops the first `n`, or every element when fewer are held, and answers them in order. The kept keep their keys. */
  dropFirst(n: number): T[] {
    const count = Math.min(n, this.length);
    const fromHead = Math.min(count, this.head.length);
    // The head is reversed, so its first elements are at its end.
    const dropped = this.head.splice(this.head.length - fromHead).toReversed();
    this.keyOffset += count;
    return dropped.concat(this.tail.splice(0, count - fromHead));
  }

  /** Drops the last `n`, or every element when fewer are held, and answers them in order. */
  dropLast(n: number): T[] {
    const count = Math.min(n, this.length);
    const fromTail = Math.min(count, this.tail.length);
    // The head is reversed, so its last elements are at its start.
    const dropped = this.head.splice(0, count - fromTail).toReversed();
    return dropped.concat(this.tail.splice(this.tail.length - fromTail));
  }
}

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

/**
 * Shared types
 */

export enum DurationUnit {
  Minutes = 'minutes',
  Hours = 'hours',
  Days = 'days',
  Weeks = 'weeks',
  Months = 'moths',
}

export class Duration {
  value: number;

  unit: DurationUnit;

  constructor(value: number, unit: DurationUnit) {
    this.value = value;
    this.unit = unit;
  }

  toISOString() {
    switch (this.unit) {
      case DurationUnit.Minutes:
        return `PT${this.value}M`;
      case DurationUnit.Hours:
        return `PT${this.value}H`;
      case DurationUnit.Days:
        return `P${this.value}D`;
      case DurationUnit.Weeks:
        return `P${this.value}W`;
      case DurationUnit.Months:
        return `P${this.value}M`;
      default:
        throw new Error('not implemented');
    }
  }
}

export type DateRangeDropdownOnChangeArgs = {
  since: Date | Duration | null;
  until: Date | null;
};

export const DateRangeDropdown = () => <div>DateRangeDropdown</div>;

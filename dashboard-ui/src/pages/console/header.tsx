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

import type { CheckedState } from '@radix-ui/react-checkbox';
import { useAtom } from 'jotai';
import {
  History as HistoryIcon,
  PanelLeftOpen as PanelLeftOpenIcon,
  Pause as PauseIcon,
  Play as PlayIcon,
  Settings as SettingsIcon,
  SkipBack as SkipBackIcon,
  SkipForward as SkipForwardIcon,
} from 'lucide-react';
import { useCallback, useContext, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Input } from '@kubetail/ui/elements/input';
import { Label } from '@kubetail/ui/elements/label';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';

import { DateRangeDropdown } from '@/components/widgets/DateRangeDropdown';
import type { DateRangeDropdownOnChangeArgs } from '@/components/widgets/DateRangeDropdown';
import { useLogViewerState } from '@/components/widgets/log-viewer';
import { cn } from '@/lib/util';

import { ALL_VIEWER_COLUMNS, PageContext } from './shared';
import type { ViewerColumn } from './shared';
import { isFollowAtom, isWrapAtom, visibleColsAtom } from './state';

/**
 * Settings button
 */

const SettingsButton = () => {
  const [visibleCols, setVisibleCols] = useAtom(visibleColsAtom);
  const [isWrap, setIsWrap] = useAtom(isWrapAtom);

  const handleOnChange = useCallback(
    (col: ViewerColumn, checked: CheckedState) => {
      const newSet = new Set(visibleCols);
      if (checked) newSet.add(col);
      else newSet.delete(col);
      setVisibleCols(newSet);
    },
    [visibleCols, setVisibleCols],
  );

  const checkboxEls = useMemo(
    () =>
      ALL_VIEWER_COLUMNS.map((col) => (
        <div key={col} className="flex items-center space-x-2">
          <Label>
            <Checkbox checked={visibleCols.has(col)} onCheckedChange={(value) => handleOnChange(col, value)} />
            {col}
          </Label>
        </div>
      )),
    [visibleCols],
  );

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="rounded-lg h-10 w-10 flex items-center justify-center enabled:hover:bg-chrome-200 disabled:opacity-30"
          title="Settings"
          aria-label="Settings"
        >
          <SettingsIcon size={18} strokeWidth={1.5} />
        </button>
      </PopoverTrigger>
      <PopoverContent
        className="bg-background w-auto mr-1 text-sm"
        onOpenAutoFocus={(ev) => ev.preventDefault()}
        sideOffset={-1}
      >
        <div className="border-b mb-1">Columns:</div>
        <div className="space-y-2">{checkboxEls}</div>
        <div className="border-b mt-2 mb-1">Options:</div>
        <div className="flex items-center space-x-1">
          <Label>
            <Checkbox checked={isWrap} onCheckedChange={(checked) => setIsWrap(!!checked)} />
            Wrap
          </Label>
        </div>
      </PopoverContent>
    </Popover>
  );
};

/**
 * Header component
 */

export function Header() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { logServerClient, shouldUseClusterAPI, isSidebarOpen, setIsSidebarOpen, logViewerRef } =
    useContext(PageContext);

  const { isLoading } = useLogViewerState(logViewerRef, [logServerClient]);
  const [isFollow, setIsFollow] = useAtom(isFollowAtom);

  const buttonCN =
    'rounded-lg h-[40px] w-[40px] flex items-center justify-center enabled:hover:bg-chrome-200 disabled:opacity-30';

  const handleDateRangeDropdownChange = useCallback(
    (args: DateRangeDropdownOnChangeArgs) => {
      if (args.since) {
        // Update location
        const cursor = args.since.toISOString();
        searchParams.set('mode', 'cursor');
        searchParams.set('cursor', cursor);
        setSearchParams(new URLSearchParams(searchParams), { replace: true });

        // Execute command
        logViewerRef.current?.jumpToCursor(cursor);
      }
    },
    [searchParams],
  );

  const handleJumpToBeginningPress = useCallback(async () => {
    // Update location
    searchParams.set('mode', 'head');
    searchParams.delete('cursor');
    setSearchParams(searchParams, { replace: true });

    // Execute command
    await logViewerRef.current?.jumpToBeginning();
  }, []);

  const handleJumpToEndPress = useCallback(async () => {
    // Update location
    searchParams.set('mode', 'tail');
    searchParams.delete('cursor');
    setSearchParams(new URLSearchParams(searchParams), { replace: true });

    // Execute command
    await logViewerRef.current?.jumpToEnd();
  }, []);

  const handlePlayPress = useCallback(() => {
    setIsFollow(true);
  }, []);

  const handlePausePress = useCallback(() => {
    setIsFollow(false);
  }, []);

  const handleSubmit = useCallback(
    (ev: React.FormEvent<HTMLFormElement>) => {
      ev.preventDefault();
      const grep = new FormData(ev.currentTarget).get('grep')?.toString().trim() || '';

      if (grep) searchParams.set('grep', grep);
      else searchParams.delete('grep');

      setSearchParams(searchParams, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  return (
    <div className="flex justify-between items-end p-1">
      <div className="flex items-center">
        {!isSidebarOpen && (
          <button type="button" onClick={() => setIsSidebarOpen(true)} title="Collapse sidebar" className="pr-2">
            <PanelLeftOpenIcon size={20} strokeWidth={2} className="text-chrome-500" />
          </button>
        )}
        <div className={cn('flex', isSidebarOpen ? 'px-4' : 'px-2')}>
          <DateRangeDropdown onChange={handleDateRangeDropdownChange}>
            <button
              type="button"
              className={buttonCN}
              title="Jump to time"
              aria-label="Jump to time"
              disabled={isLoading}
            >
              <HistoryIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
            </button>
          </DateRangeDropdown>
          <button
            type="button"
            className={buttonCN}
            title="Jump to beginning"
            aria-label="Jump to beginning"
            onClick={handleJumpToBeginningPress}
            disabled={isLoading}
          >
            <SkipBackIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
          {isFollow ? (
            <button
              type="button"
              className={buttonCN}
              title="Pause"
              aria-label="Pause"
              onClick={handlePausePress}
              disabled={isLoading}
            >
              <PauseIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
            </button>
          ) : (
            <button
              type="button"
              className={buttonCN}
              title="Play"
              aria-label="Play"
              onClick={handlePlayPress}
              disabled={isLoading}
            >
              <PlayIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
            </button>
          )}
          <button
            type="button"
            className={buttonCN}
            title="Jump to end"
            aria-label="Jump to end"
            onClick={handleJumpToEndPress}
            disabled={isLoading}
          >
            <SkipForwardIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        </div>
        <div>
          {shouldUseClusterAPI && (
            <form onSubmit={handleSubmit}>
              <Input
                name="grep"
                className="w-100 bg-background"
                placeholder="Match string or /regex/..."
                defaultValue={searchParams.get('grep') || ''}
                disabled={isLoading}
              />
            </form>
          )}
        </div>
      </div>
      <div className="h-full flex flex-col justify-end items-end">
        <SettingsButton />
      </div>
    </div>
  );
}

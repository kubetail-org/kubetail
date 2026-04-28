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

import { useAtomValue } from 'jotai';
import { ChevronRight as ChevronRightIcon, Info as InfoIcon } from 'lucide-react';
import { useCallback, useContext, useState } from 'react';

import { Button } from '@kubetail/ui/elements/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@kubetail/ui/elements/dialog';
import { Input } from '@kubetail/ui/elements/input';
import { Label } from '@kubetail/ui/elements/label';
import { Tooltip, TooltipContent, TooltipTrigger } from '@kubetail/ui/elements/tooltip';

import appConfig from '@/app-config';
import { parseTimestamp } from '@/components/widgets/DateRangeDropdown';
import { getCsrfToken, waitForCsrfToken } from '@/lib/auth';
import { LogRecordsQueryMode, LogSourceFilter } from '@/lib/graphql/dashboard/__generated__/graphql';
import { useTimezone } from '@/lib/timezone';
import { ClusterAPIProxyPathInput, clusterAPIProxyPath, getBasename, joinPaths } from '@/lib/util';

import { PageContext, ViewerColumn, viewerColumnToBackend } from './shared';
import { visibleColsAtom } from './state';

const DOWNLOAD_PATH = 'api/v1/download';

/**
 * Types
 */

export type DownloadOutputFormat = 'CSV' | 'TSV' | 'TEXT';
export type DownloadMessageFormat = 'TEXT' | 'ANSI';

export type RangeMode = 'entire' | 'first' | 'last' | 'between';
export type MessageFormat = 'text' | 'ansi';
export type ContentMode = 'metadata' | 'raw';
export type OutputFormat = 'csv' | 'tsv';

export type DialogState = {
  rangeMode: RangeMode;
  firstN: string;
  lastN: string;
  since: string;
  until: string;
  messageFormat: MessageFormat;
  contentMode: ContentMode;
  outputFormat: OutputFormat;
};

export type DownloadArgs = {
  mode: LogRecordsQueryMode;
  limit: number | null;
  since?: string;
  until?: string;
  outputFormat: DownloadOutputFormat;
  messageFormat: DownloadMessageFormat;
  includeMetadata: boolean;
  columns?: string[];
};

export type DownloadFilters = {
  kubeContext: string;
  sources: string[];
  sourceFilter?: LogSourceFilter;
  grep?: string;
};

/**
 * Helpers
 */

// ISO 8601 duration: starts with P or PT (e.g. PT1M, P1D, P1Y2M)
const DURATION_RE = /^P/i;

export function normalizeTimeArg(input: string, timezone: string): string | undefined {
  const trimmed = input.trim();
  if (!trimmed) return undefined;
  if (DURATION_RE.test(trimmed)) return trimmed;
  const date = parseTimestamp(trimmed, timezone);
  if (!date) return undefined;
  return date.toISOString();
}

type RangeArgs = Pick<DownloadArgs, 'mode' | 'limit' | 'since' | 'until'>;

function buildRange(state: DialogState, timezone: string): RangeArgs {
  switch (state.rangeMode) {
    case 'first':
      return { mode: LogRecordsQueryMode.Head, limit: Number(state.firstN) || 0 };
    case 'last':
      return { mode: LogRecordsQueryMode.Tail, limit: Number(state.lastN) || 0 };
    case 'between':
      return {
        mode: LogRecordsQueryMode.Head,
        limit: null,
        since: normalizeTimeArg(state.since, timezone),
        until: normalizeTimeArg(state.until, timezone),
      };
    case 'entire':
    default:
      return { mode: LogRecordsQueryMode.Head, limit: null };
  }
}

export function buildDownloadArgs(state: DialogState, visibleCols: Set<ViewerColumn>, timezone: string): DownloadArgs {
  const range = buildRange(state, timezone);
  const messageFormat: DownloadMessageFormat = state.messageFormat === 'ansi' ? 'ANSI' : 'TEXT';

  if (state.contentMode === 'raw') {
    return {
      ...range,
      outputFormat: 'TEXT',
      messageFormat,
      includeMetadata: false,
    };
  }

  return {
    ...range,
    outputFormat: state.outputFormat === 'csv' ? 'CSV' : 'TSV',
    messageFormat,
    includeMetadata: true,
    columns: Array.from(visibleCols)
      .map((col) => viewerColumnToBackend(col))
      .filter((name): name is string => name !== undefined),
  };
}

/**
 * getDownloadActionURL - Resolve the POST target for the download form.
 */

export type DownloadEndpointInput = ClusterAPIProxyPathInput & {
  shouldUseClusterAPI: boolean;
};

export function getDownloadActionURL(input: DownloadEndpointInput): string {
  if (input.shouldUseClusterAPI) return clusterAPIProxyPath(input, DOWNLOAD_PATH);
  return joinPaths(input.basename, DOWNLOAD_PATH);
}

/**
 * submitLogDownload - POST an HTML form to the dashboard download endpoint,
 * targeting a fresh hidden iframe so the browser streams the response to disk
 * without navigating the current tab. A new iframe per submission prevents
 * concurrent downloads from cancelling each other.
 *
 * The iframe is left in the DOM after submission: attachment responses abort
 * iframe navigation so no `load` event is guaranteed, and removing early on
 * the initial `about:blank` load resolves the form's target to the current
 * window and causes a white flash during the aborted navigation.
 */

let downloadCounter = 0;

function createDownloadIframe(): HTMLIFrameElement {
  downloadCounter += 1;
  const iframe = document.createElement('iframe');
  iframe.name = `kubetail-download-${Date.now()}-${downloadCounter}`;
  iframe.style.display = 'none';
  document.body.appendChild(iframe);
  return iframe;
}

export function submitLogDownload(action: string, filters: DownloadFilters, args: DownloadArgs): void {
  const iframe = createDownloadIframe();

  const form = document.createElement('form');
  form.method = 'POST';
  form.action = action;
  form.target = iframe.name;
  form.style.display = 'none';

  const append = (name: string, value: string) => {
    const input = document.createElement('input');
    input.type = 'hidden';
    input.name = name;
    input.value = value;
    form.appendChild(input);
  };

  if (filters.kubeContext) append('kubeContext', filters.kubeContext);
  filters.sources.forEach((s) => append('sources', s));
  if (filters.grep) append('grep', filters.grep);
  if (filters.sourceFilter) {
    const sf = filters.sourceFilter;
    (sf.region ?? []).forEach((v) => append('sourceFilter.region', v));
    (sf.zone ?? []).forEach((v) => append('sourceFilter.zone', v));
    (sf.os ?? []).forEach((v) => append('sourceFilter.os', v));
    (sf.arch ?? []).forEach((v) => append('sourceFilter.arch', v));
    (sf.node ?? []).forEach((v) => append('sourceFilter.node', v));
    (sf.container ?? []).forEach((v) => append('sourceFilter.container', v));
  }

  append('mode', args.mode);
  if (args.limit != null) append('limit', String(args.limit));
  if (args.since) append('since', args.since);
  if (args.until) append('until', args.until);
  append('outputFormat', args.outputFormat);
  append('messageFormat', args.messageFormat);
  append('includeMetadata', String(args.includeMetadata));
  (args.columns ?? []).forEach((c) => append('columns', c));

  // HTML form submission can't set headers, so deliver the CSRF token via a
  // hidden form field. The server middleware accepts this fallback for
  // form-encoded POSTs.
  append('csrfToken', getCsrfToken());

  document.body.appendChild(form);
  try {
    form.submit();
  } finally {
    form.remove();
  }
}

/**
 * Dialog component
 */

const radioRowCN = 'flex items-center gap-2 text-sm';
const radioInputCN = 'h-4 w-4 accent-primary';

type DownloadDialogProps = React.ComponentProps<typeof Dialog> & {
  onOpenChange: (open: boolean) => void;
};

export const DownloadDialog = (props: DownloadDialogProps) => {
  const { onOpenChange } = props;
  const { logServerClient, shouldUseClusterAPI, kubeContext } = useContext(PageContext);
  const visibleCols = useAtomValue(visibleColsAtom);
  const [timezone] = useTimezone();

  const [rangeMode, setRangeMode] = useState<RangeMode>('entire');
  const [firstN, setFirstN] = useState('100');
  const [lastN, setLastN] = useState('100');
  const [since, setSince] = useState('');
  const [until, setUntil] = useState('');
  const [messageFormat, setMessageFormat] = useState<MessageFormat>('text');
  const [contentMode, setContentMode] = useState<ContentMode>('metadata');
  const [outputFormat, setOutputFormat] = useState<OutputFormat>('tsv');
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const handleSubmit = useCallback(async () => {
    if (!logServerClient) return;
    const args = buildDownloadArgs(
      { rangeMode, firstN, lastN, since, until, messageFormat, contentMode, outputFormat },
      visibleCols,
      timezone,
    );

    const { kubeContext: clientKubeContext, sources, sourceFilter, grep } = logServerClient;
    const downloadSource = { kubeContext: clientKubeContext, sources, sourceFilter, grep };
    const action = getDownloadActionURL({
      basename: getBasename(),
      environment: appConfig.environment,
      shouldUseClusterAPI: !!shouldUseClusterAPI,
      kubeContext: kubeContext ?? '',
    });
    await waitForCsrfToken();
    submitLogDownload(action, downloadSource, args);

    onOpenChange(false);
  }, [
    logServerClient,
    shouldUseClusterAPI,
    kubeContext,
    rangeMode,
    firstN,
    lastN,
    since,
    until,
    messageFormat,
    contentMode,
    outputFormat,
    visibleCols,
    timezone,
    onOpenChange,
  ]);

  return (
    <Dialog {...props}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Download logs</DialogTitle>
          <DialogDescription>Choose what to include in the downloaded file.</DialogDescription>
        </DialogHeader>
        <div className="space-y-5 py-2">
          <section className="space-y-2">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Range</h3>
            <div className="space-y-2">
              <Label className={radioRowCN}>
                <input
                  type="radio"
                  name="download-range"
                  className={radioInputCN}
                  checked={rangeMode === 'entire'}
                  onChange={() => setRangeMode('entire')}
                />
                Entire stream (beginning to end)
              </Label>
              <Label className={radioRowCN}>
                <input
                  type="radio"
                  name="download-range"
                  className={radioInputCN}
                  checked={rangeMode === 'first'}
                  onChange={() => setRangeMode('first')}
                />
                First
                <Input
                  type="number"
                  min="1"
                  className="w-24 h-8"
                  value={firstN}
                  onChange={(ev) => setFirstN(ev.target.value)}
                  onFocus={() => setRangeMode('first')}
                />
                lines
              </Label>
              <Label className={radioRowCN}>
                <input
                  type="radio"
                  name="download-range"
                  className={radioInputCN}
                  checked={rangeMode === 'last'}
                  onChange={() => setRangeMode('last')}
                />
                Last
                <Input
                  type="number"
                  min="1"
                  className="w-24 h-8"
                  value={lastN}
                  onChange={(ev) => setLastN(ev.target.value)}
                  onFocus={() => setRangeMode('last')}
                />
                lines
              </Label>
              <div className="space-y-2">
                <Label className={radioRowCN}>
                  <input
                    type="radio"
                    name="download-range"
                    className={radioInputCN}
                    checked={rangeMode === 'between'}
                    onChange={() => setRangeMode('between')}
                  />
                  Between
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        className="text-muted-foreground hover:text-foreground"
                        aria-label="Supported timestamp formats"
                      >
                        <InfoIcon size={14} />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="right" className="max-w-none">
                      <div className="font-semibold mb-1">Supported formats</div>
                      <ul className="font-mono text-xs space-y-0.5 whitespace-nowrap">
                        <li title="ISO 8601 / RFC 3339">2006-01-02T15:04:05+07:00</li>
                        <li title="RFC 1123 / RFC 2822">Mon, 02 Jan 2006 15:04:05 -0700</li>
                        <li title="Apache CLF">02/Jan/2006:15:04:05 -0700</li>
                        <li title="Unix timestamp (seconds)">1136214245</li>
                        <li title="Unix timestamp (milliseconds)">1776393600000</li>
                        <li title="ISO 8601 duration">PT1M</li>
                      </ul>
                    </TooltipContent>
                  </Tooltip>
                </Label>
                <div className="flex items-center gap-2 pl-6 text-sm">
                  <Input
                    type="text"
                    placeholder="since"
                    className="flex-1 h-8"
                    value={since}
                    onChange={(ev) => setSince(ev.target.value)}
                    onFocus={() => setRangeMode('between')}
                  />
                  and
                  <Input
                    type="text"
                    placeholder="until"
                    className="flex-1 h-8"
                    value={until}
                    onChange={(ev) => setUntil(ev.target.value)}
                    onFocus={() => setRangeMode('between')}
                  />
                </div>
              </div>
            </div>
          </section>

          <section className="space-y-2">
            <button
              type="button"
              className="flex items-center gap-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground hover:text-foreground"
              onClick={() => setAdvancedOpen((v) => !v)}
              aria-expanded={advancedOpen}
            >
              <ChevronRightIcon size={14} className={`transition-transform ${advancedOpen ? 'rotate-90' : ''}`} />
              Advanced
            </button>
            {advancedOpen && (
              <div className="space-y-5 pt-2">
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Message format
                  </h4>
                  <div className="space-y-2">
                    <Label className={radioRowCN}>
                      <input
                        type="radio"
                        name="download-message-format"
                        className={radioInputCN}
                        checked={messageFormat === 'text'}
                        onChange={() => setMessageFormat('text')}
                      />
                      With message (text)
                    </Label>
                    <Label className={radioRowCN}>
                      <input
                        type="radio"
                        name="download-message-format"
                        className={radioInputCN}
                        checked={messageFormat === 'ansi'}
                        onChange={() => setMessageFormat('ansi')}
                      />
                      With message (ANSI)
                    </Label>
                  </div>
                </div>

                <div className="space-y-2">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Contents</h4>
                  <div className="space-y-2">
                    <Label className={radioRowCN}>
                      <input
                        type="radio"
                        name="download-content"
                        className={radioInputCN}
                        checked={contentMode === 'metadata'}
                        onChange={() => setContentMode('metadata')}
                      />
                      With metadata (columns in viewer)
                    </Label>
                    <Label className={radioRowCN}>
                      <input
                        type="radio"
                        name="download-content"
                        className={radioInputCN}
                        checked={contentMode === 'raw'}
                        onChange={() => setContentMode('raw')}
                      />
                      Raw data (just the messages)
                    </Label>
                  </div>
                </div>

                <div className="space-y-2">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Output format
                  </h4>
                  <div className="space-y-2">
                    <Label className={radioRowCN}>
                      <input
                        type="radio"
                        name="download-output-format"
                        className={radioInputCN}
                        checked={outputFormat === 'tsv'}
                        onChange={() => setOutputFormat('tsv')}
                      />
                      TSV
                    </Label>
                    <Label className={radioRowCN}>
                      <input
                        type="radio"
                        name="download-output-format"
                        className={radioInputCN}
                        checked={outputFormat === 'csv'}
                        onChange={() => setOutputFormat('csv')}
                      />
                      CSV
                    </Label>
                  </div>
                </div>
              </div>
            )}
          </section>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary">Cancel</Button>
          </DialogClose>
          <Button onClick={handleSubmit} disabled={!logServerClient}>
            Download
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

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

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createStore, Provider } from 'jotai';

import type { ApolloClient } from '@apollo/client';

import { createMockLogViewerHandle } from '@/components/widgets/log-viewer/mock';
import { LogRecordsQueryMode } from '@/lib/graphql/dashboard/__generated__/graphql';

import { DownloadDialog, buildDownloadArgs, normalizeTimeArg, submitLogDownload } from './download';
import { LogServerClient } from './log-server-client';
import { PageContext, ViewerColumn, viewerColumnToBackend } from './shared';

vi.mock('@/lib/timezone', () => ({
  useTimezone: () => ['UTC', vi.fn()],
}));

/**
 * Helper pure-function tests
 */

describe('normalizeTimeArg', () => {
  it('returns undefined on empty input', () => {
    expect(normalizeTimeArg('', 'UTC')).toBeUndefined();
    expect(normalizeTimeArg('   ', 'UTC')).toBeUndefined();
  });

  it('passes ISO 8601 durations through unchanged', () => {
    expect(normalizeTimeArg('PT1M', 'UTC')).toBe('PT1M');
    expect(normalizeTimeArg('P1D', 'UTC')).toBe('P1D');
    expect(normalizeTimeArg('pt5h', 'UTC')).toBe('pt5h');
  });

  it('parses ISO timestamps to ISO string', () => {
    expect(normalizeTimeArg('2024-01-02T15:04:05Z', 'UTC')).toBe('2024-01-02T15:04:05.000Z');
  });

  it('returns undefined on unparseable input', () => {
    expect(normalizeTimeArg('not-a-time', 'UTC')).toBeUndefined();
  });
});

describe('viewerColumnToBackend', () => {
  it('maps viewer columns to backend column names', () => {
    expect(viewerColumnToBackend(ViewerColumn.Timestamp)).toBe('timestamp');
    expect(viewerColumnToBackend(ViewerColumn.Pod)).toBe('pod');
    expect(viewerColumnToBackend(ViewerColumn.Container)).toBe('container');
    expect(viewerColumnToBackend(ViewerColumn.Region)).toBe('region');
    expect(viewerColumnToBackend(ViewerColumn.Zone)).toBe('zone');
    expect(viewerColumnToBackend(ViewerColumn.OS)).toBe('os');
    expect(viewerColumnToBackend(ViewerColumn.Arch)).toBe('arch');
    expect(viewerColumnToBackend(ViewerColumn.Node)).toBe('node');
    expect(viewerColumnToBackend(ViewerColumn.Message)).toBe('message');
  });

  it('returns undefined for ColorDot (not persistable)', () => {
    expect(viewerColumnToBackend(ViewerColumn.ColorDot)).toBeUndefined();
  });
});

describe('buildDownloadArgs', () => {
  const visibleCols = new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Pod, ViewerColumn.Message]);

  it('entire stream → mode HEAD, no limit', () => {
    const args = buildDownloadArgs(
      {
        rangeMode: 'entire',
        firstN: '100',
        lastN: '100',
        since: '',
        until: '',
        messageFormat: 'text',
        contentMode: 'metadata',
        outputFormat: 'tsv',
      },
      visibleCols,
      'UTC',
    );
    expect(args.mode).toBe(LogRecordsQueryMode.Head);
    expect(args.limit).toBeNull();
    expect(args.since).toBeUndefined();
    expect(args.until).toBeUndefined();
  });

  it('first N → mode HEAD, limit N', () => {
    const args = buildDownloadArgs(
      {
        rangeMode: 'first',
        firstN: '500',
        lastN: '100',
        since: '',
        until: '',
        messageFormat: 'text',
        contentMode: 'metadata',
        outputFormat: 'tsv',
      },
      visibleCols,
      'UTC',
    );
    expect(args.mode).toBe(LogRecordsQueryMode.Head);
    expect(args.limit).toBe(500);
  });

  it('last N → mode TAIL, limit N', () => {
    const args = buildDownloadArgs(
      {
        rangeMode: 'last',
        firstN: '100',
        lastN: '250',
        since: '',
        until: '',
        messageFormat: 'text',
        contentMode: 'metadata',
        outputFormat: 'tsv',
      },
      visibleCols,
      'UTC',
    );
    expect(args.mode).toBe(LogRecordsQueryMode.Tail);
    expect(args.limit).toBe(250);
  });

  it('between → mode HEAD, since/until normalized', () => {
    const args = buildDownloadArgs(
      {
        rangeMode: 'between',
        firstN: '100',
        lastN: '100',
        since: 'PT1M',
        until: '2024-01-02T15:04:05Z',
        messageFormat: 'text',
        contentMode: 'metadata',
        outputFormat: 'tsv',
      },
      visibleCols,
      'UTC',
    );
    expect(args.mode).toBe(LogRecordsQueryMode.Head);
    expect(args.limit).toBeNull();
    expect(args.since).toBe('PT1M');
    expect(args.until).toBe('2024-01-02T15:04:05.000Z');
  });

  it('raw content mode → includeMetadata false, outputFormat TEXT', () => {
    const args = buildDownloadArgs(
      {
        rangeMode: 'first',
        firstN: '50',
        lastN: '100',
        since: '',
        until: '',
        messageFormat: 'ansi',
        contentMode: 'raw',
        outputFormat: 'tsv',
      },
      visibleCols,
      'UTC',
    );
    expect(args.includeMetadata).toBe(false);
    expect(args.outputFormat).toBe('TEXT');
    expect(args.messageFormat).toBe('ANSI');
    expect(args.columns).toBeUndefined();
  });

  it('metadata content mode → columns derived from visibleCols, skipping ColorDot', () => {
    const args = buildDownloadArgs(
      {
        rangeMode: 'first',
        firstN: '50',
        lastN: '100',
        since: '',
        until: '',
        messageFormat: 'text',
        contentMode: 'metadata',
        outputFormat: 'csv',
      },
      visibleCols,
      'UTC',
    );
    expect(args.includeMetadata).toBe(true);
    expect(args.outputFormat).toBe('CSV');
    expect(args.columns).toEqual(['timestamp', 'pod', 'message']);
  });
});

/**
 * submitLogDownload tests
 */

function readFormFields(form: HTMLFormElement): [string, string][] {
  return Array.from(form.querySelectorAll('input[type="hidden"]')).map((el) => {
    const input = el as HTMLInputElement;
    return [input.name, input.value] as [string, string];
  });
}

describe('submitLogDownload', () => {
  let submitSpy: ReturnType<typeof vi.spyOn>;
  let submittedForm: HTMLFormElement | null = null;

  beforeEach(() => {
    submittedForm = null;
    submitSpy = vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function mock(this: HTMLFormElement) {
      submittedForm = this.cloneNode(true) as HTMLFormElement;
    });
  });

  afterEach(() => {
    submitSpy.mockRestore();
    document.body.innerHTML = '';
  });

  it('POSTs to /api/logs/download targeting a distinct hidden iframe', () => {
    submitLogDownload(
      { kubeContext: 'ctx-1', sources: ['ns/pod/c'] },
      {
        mode: LogRecordsQueryMode.Head,
        limit: 100,
        outputFormat: 'TSV',
        messageFormat: 'TEXT',
        includeMetadata: true,
        columns: ['timestamp', 'message'],
      },
    );

    expect(submitSpy).toHaveBeenCalledTimes(1);
    expect(submittedForm!.method.toLowerCase()).toBe('post');
    expect(submittedForm!.action).toMatch(/\/api\/logs\/download$/);
    const iframe = document.querySelector<HTMLIFrameElement>(`iframe[name="${submittedForm!.target}"]`);
    expect(iframe).not.toBeNull();
    expect(iframe!.style.display).toBe('none');
  });

  it('emits filters + args as hidden fields with arrays repeated', () => {
    submitLogDownload(
      {
        kubeContext: 'ctx-1',
        sources: ['ns/pod-a/c1', 'ns/pod-b/c2'],
        sourceFilter: { region: ['us-east-1'], container: ['app'] } as never,
        grep: 'ERROR',
      },
      {
        mode: LogRecordsQueryMode.Tail,
        limit: 250,
        since: '2024-01-01T00:00:00Z',
        outputFormat: 'CSV',
        messageFormat: 'ANSI',
        includeMetadata: true,
        columns: ['timestamp', 'pod', 'message'],
      },
    );

    const fields = readFormFields(submittedForm!);
    expect(fields).toEqual(
      expect.arrayContaining([
        ['kubeContext', 'ctx-1'],
        ['sources', 'ns/pod-a/c1'],
        ['sources', 'ns/pod-b/c2'],
        ['sourceFilter.region', 'us-east-1'],
        ['sourceFilter.container', 'app'],
        ['grep', 'ERROR'],
        ['mode', LogRecordsQueryMode.Tail],
        ['limit', '250'],
        ['since', '2024-01-01T00:00:00Z'],
        ['outputFormat', 'CSV'],
        ['messageFormat', 'ANSI'],
        ['includeMetadata', 'true'],
        ['columns', 'timestamp'],
        ['columns', 'pod'],
        ['columns', 'message'],
      ]),
    );
    expect(fields.some(([k, v]) => k === 'limit' && v === 'null')).toBe(false);
  });

  it('omits limit and columns when absent', () => {
    submitLogDownload(
      { kubeContext: '', sources: ['ns/pod/c'] },
      {
        mode: LogRecordsQueryMode.Head,
        limit: null,
        outputFormat: 'TEXT',
        messageFormat: 'TEXT',
        includeMetadata: false,
      },
    );

    const fields = readFormFields(submittedForm!);
    expect(fields.some(([k]) => k === 'limit')).toBe(false);
    expect(fields.some(([k]) => k === 'columns')).toBe(false);
  });

  it('creates a distinct iframe per call so concurrent downloads do not cancel', () => {
    const call = () =>
      submitLogDownload(
        { kubeContext: 'ctx', sources: ['ns/pod/c'] },
        {
          mode: LogRecordsQueryMode.Head,
          limit: null,
          outputFormat: 'TSV',
          messageFormat: 'TEXT',
          includeMetadata: true,
          columns: ['message'],
        },
      );

    call();
    call();

    const iframes = Array.from(document.querySelectorAll('iframe'));
    expect(iframes.length).toBe(2);
    expect(new Set(iframes.map((f) => f.name)).size).toBe(2);
  });
});

/**
 * Dialog component tests
 */

const renderDialog = () => {
  const client = new LogServerClient({
    apolloClient: {} as ApolloClient,
    kubeContext: 'ctx-1',
    sources: ['ns/pod-a/c1'],
    grep: 'ERROR',
  });
  const store = createStore();
  const onOpenChange = vi.fn();
  const contextValue = {
    kubeContext: 'ctx-1',
    shouldUseClusterAPI: undefined,
    logServerClient: client,
    grep: 'ERROR',
    logViewerRef: { current: createMockLogViewerHandle() },
    isSidebarOpen: true,
    setIsSidebarOpen: vi.fn(),
  };

  render(
    <Provider store={store}>
      <PageContext.Provider value={contextValue}>
        <DownloadDialog open onOpenChange={onOpenChange} />
      </PageContext.Provider>
    </Provider>,
  );

  return { onOpenChange };
};

describe('DownloadDialog', () => {
  let submitSpy: ReturnType<typeof vi.spyOn>;
  let submittedForm: HTMLFormElement | null = null;

  beforeEach(() => {
    submittedForm = null;
    submitSpy = vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function mock(this: HTMLFormElement) {
      submittedForm = this.cloneNode(true) as HTMLFormElement;
    });
  });

  afterEach(() => {
    submitSpy.mockRestore();
    // Leave RTL's mount container alone — it needs it for cleanup. Just drop
    // the orphan iframes submitLogDownload appended.
    document.querySelectorAll('iframe[name^="kubetail-download-"]').forEach((f) => f.remove());
  });

  it('submits default first-100 TSV download with filters from the client and closes', async () => {
    const { onOpenChange } = renderDialog();

    fireEvent.click(screen.getByRole('radio', { name: /^First/ }));
    fireEvent.click(screen.getByRole('button', { name: /^Download$/ }));

    await waitFor(() => expect(submitSpy).toHaveBeenCalledTimes(1));
    const fields = readFormFields(submittedForm!);
    expect(fields).toEqual(
      expect.arrayContaining([
        ['kubeContext', 'ctx-1'],
        ['sources', 'ns/pod-a/c1'],
        ['grep', 'ERROR'],
        ['mode', 'HEAD'],
        ['limit', '100'],
        ['outputFormat', 'TSV'],
        ['messageFormat', 'TEXT'],
        ['includeMetadata', 'true'],
      ]),
    );
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it('raw mode submits TEXT outputFormat and includeMetadata false', async () => {
    renderDialog();

    fireEvent.click(screen.getByRole('button', { name: /Advanced/ }));
    fireEvent.click(await screen.findByRole('radio', { name: /Raw data/ }));
    fireEvent.click(screen.getByRole('button', { name: /^Download$/ }));

    await waitFor(() => expect(submitSpy).toHaveBeenCalled());
    const fields = readFormFields(submittedForm!);
    expect(fields).toEqual(
      expect.arrayContaining([
        ['outputFormat', 'TEXT'],
        ['includeMetadata', 'false'],
      ]),
    );
  });
});

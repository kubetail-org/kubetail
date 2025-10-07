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

import { useSubscription } from '@apollo/client';
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table';
import type {
  ColumnDef,
  Row,
  SortDirection,
  SortingState,
  Table as TableType,
  TableMeta,
  TableOptions,
} from '@tanstack/react-table';
import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { ChevronDown, ChevronUp, ExternalLink, Layers3, PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react';
import numeral from 'numeral';
import { createContext, memo, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import TimeAgo from 'react-timeago';
import type { Formatter, Suffix, Unit } from 'react-timeago';
import { useDebounceCallback } from 'usehooks-ts';

import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { SearchBox } from '@kubetail/ui/elements/search-box';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@kubetail/ui/elements/table';

import appConfig from '@/app-config';
import KubetailLogo from '@/assets/logo.svg?react';
import AdaptiveTimeAgo from '@/components/widgets/AdaptiveTimeAgo';
import KubeContextPicker from '@/components/widgets/KubeContextPicker';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import SettingsDropdown from '@/components/widgets/SettingsDropdown';
import {
  HOME_NAMESPACES_LIST_FETCH,
  HOME_NAMESPACES_LIST_WATCH,
  KUBERNETES_API_READY_WAIT,
} from '@/lib/graphql/dashboard/ops';
import { useIsClusterAPIEnabled, useListQueryWithSubscription } from '@/lib/hooks';
import { joinPaths, getBasename, cn } from '@/lib/util';
import { WorkloadKind, ALL_WORKLOAD_KINDS, GLYPH_ICON_MAP, KNOCKOUT_ICON_MAP, PLURAL_LABEL_MAP } from '@/lib/workload';

import { LogMetadataProvider } from './log-metadata-provider';
import {
  filteredWorkloadCountAtomFamilies,
  filteredTotalCountAtomFamily,
  isLoadingAtomFamily,
  namespaceFilterAtom,
  searchQueryAtom,
  workloadIsFetchingAtomFamilies,
  filteredWorkloadItemsAtomFamilies,
} from './state';
import { useLogFileInfo } from './util';
import { WorkloadDataProvider } from './workload-data-provider';

/**
 * Shared variables and helper methods
 */

const basename = getBasename();

const defaultKubeContext = appConfig.environment === 'cluster' ? '' : null;

type ContextType = {
  kubeContext: string | null;
  setKubeContext: React.Dispatch<React.SetStateAction<string | null>>;
  workloadKindFilter?: WorkloadKind;
  setWorkloadKindFilter: React.Dispatch<React.SetStateAction<WorkloadKind | undefined>>;
  sidebarOpen: boolean;
  setSidebarOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

const Context = createContext({} as ContextType);

/**
 * DisplayWorkloadItems component
 */

const lastModifiedAtFormatter: Formatter = (
  value: number,
  unit: Unit,
  suffix: Suffix,
  epochMilliseconds: number,
  nextFormatter: Formatter,
  now: () => number,
) => {
  if (suffix === 'from now' || unit === 'second') return 'just now';
  if (nextFormatter) return nextFormatter(value, unit, suffix, epochMilliseconds, nextFormatter, now);
  return '';
};

type WorkloadTableData = {
  id: string;
  name: string;
  namespace: string;
  createdAt: Date;
  size?: number;
  lastModifiedAt?: Date;
  sourceString: string;
  containerIDs: string[];
};

interface WorkloadTableMeta extends TableMeta<WorkloadTableData> {
  kubeContext: string | null;
  selectAll: boolean;
  isChecked: Map<string, boolean>;
  handleSelectAllChange: () => void;
  handleSingleCheckboxChange: (id: string) => void;
}

type TableCellProps = {
  table: TableType<WorkloadTableData>;
  row: Row<WorkloadTableData>;
};

const SizeTableCell = ({ table, row }: TableCellProps) => {
  const meta = table.options.meta as WorkloadTableMeta;

  const ids = useMemo(() => [row.original.id], []);
  const logFileInfo = useLogFileInfo(meta.kubeContext, ids);

  const info = logFileInfo.get(row.original.id);
  if (info === undefined) return <span>--</span>;

  return numeral(info.size).format('0.0 b');
};

const LastModifiedAtTableCell = ({ table, row }: TableCellProps) => {
  const meta = table.options.meta as WorkloadTableMeta;

  const ids = useMemo(() => [row.original.id], []);
  const logFileInfo = useLogFileInfo(meta.kubeContext, ids);

  const info = logFileInfo.get(row.original.id);
  if (info === undefined) return <span>--</span>;

  return (
    <TimeAgo
      date={info.lastModifiedAt}
      formatter={lastModifiedAtFormatter}
      minPeriod={60}
      title={info.lastModifiedAt.toUTCString()}
    />
  );
};

const WORKLOAD_TABLE_COLUMNS = [
  {
    id: 'checkbox',
    header: ({ table }) => {
      const meta = table.options.meta as WorkloadTableMeta;
      return (
        <div className="flex items-center">
          <Checkbox checked={meta.selectAll} onCheckedChange={meta.handleSelectAllChange} />
        </div>
      );
    },
    cell: ({ table, row }) => {
      const meta = table.options.meta as WorkloadTableMeta;
      const { id, sourceString } = row.original;
      return (
        <div className="flex items-center">
          <Checkbox
            name="source"
            value={sourceString}
            checked={meta.isChecked.get(id) || false}
            onCheckedChange={() => meta.handleSingleCheckboxChange(id)}
          />
        </div>
      );
    },
  },
  {
    accessorKey: 'name',
    header: 'Name',
    enableSorting: true,
  },
  {
    accessorKey: 'namespace',
    header: 'Namespace',
    enableSorting: true,
  },
  {
    accessorKey: 'createdAt',
    enableSorting: true,
    sortDescFirst: true,
    header: 'Created',
    cell: ({ row }) => {
      const { createdAt } = row.original;
      return <AdaptiveTimeAgo date={createdAt} />;
    },
  },
  {
    accessorKey: 'size',
    enableSorting: true,
    sortDescFirst: true,
    sortUndefined: 'last',
    header: 'Size',
    cell: SizeTableCell,
  },
  {
    accessorKey: 'lastModifiedAt',
    enableSorting: true,
    sortDescFirst: true,
    sortUndefined: 'last',
    header: 'Last Event',
    cell: LastModifiedAtTableCell,
  },
  {
    id: 'viewlink',
    cell: ({ table, row }) => {
      const { kubeContext } = table.options.meta as WorkloadTableMeta;
      const { sourceString } = row.original;
      return (
        <a
          target="_blank"
          href={`${joinPaths(basename, '/console')}?kubeContext=${encodeURIComponent(kubeContext || '')}&source=${encodeURIComponent(sourceString)}`}
          className="flex items-center underline text-primary"
        >
          <div>view</div>
          <ExternalLink className="w-[18px] h-[18px] ml-1" />
        </a>
      );
    },
  },
] satisfies ColumnDef<WorkloadTableData>[];

type SortIconProps = {
  dir: SortDirection | false;
  descFirst: boolean | undefined;
};

const SortIcon = ({ dir, descFirst }: SortIconProps) => {
  const iconCN = 'h-5 w-5 ml-2 flex-none text-[currentColor]/25';

  switch (dir) {
    case 'asc':
      return <ChevronUp className={iconCN} />;
    case 'desc':
      return <ChevronDown className={iconCN} />;
    default: {
      const Icon = descFirst ? ChevronDown : ChevronUp;
      return <Icon className={cn(iconCN, 'invisible group-hover:visible group-focus:visible')} />;
    }
  }
};

const MemoizedDataTableRow = memo(
  ({ row }: { row: Row<WorkloadTableData> }) => (
    <TableRow>
      {row.getVisibleCells().map((cell) => {
        let cls = '';
        if (cell.column.id === 'lastModifiedAt') {
          cls = row.original.containerIDs.map((id) => `last_event_${id}`).join(' ');
        }
        return (
          <TableCell key={cell.id} className={cls}>
            {flexRender(cell.column.columnDef.cell, cell.getContext())}
          </TableCell>
        );
      })}
    </TableRow>
  ),
  (prevProps, nextProps) => prevProps.row.original.id === nextProps.row.original.id,
);

MemoizedDataTableRow.displayName = 'MemoizedDataTableRow';

type DisplayWorkloadItemsProps = {
  kind: WorkloadKind;
};

const DisplayWorkloadItems = memo(({ kind }: DisplayWorkloadItemsProps) => {
  const { kubeContext, workloadKindFilter } = useContext(Context);
  const isClusterAPIEnabled = useIsClusterAPIEnabled(kubeContext);

  const isFetching = useAtomValue(workloadIsFetchingAtomFamilies[kind](kubeContext));
  const items = useAtomValue(filteredWorkloadItemsAtomFamilies[kind](kubeContext));

  const itemIDs = useMemo(() => items.map((item) => item.metadata.uid), [items]);

  const logFileInfo = useLogFileInfo(kubeContext, itemIDs);

  const data = useMemo(
    () =>
      items.map((item) => {
        const fileInfo = logFileInfo.get(item.metadata.uid);
        return {
          id: item.id,
          name: item.metadata.name,
          namespace: item.metadata.namespace,
          createdAt: item.metadata.creationTimestamp,
          size: fileInfo?.size,
          lastModifiedAt: fileInfo?.lastModifiedAt,
          sourceString: `${item.metadata.namespace}:${kind}/${item.metadata.name}/*`,
          containerIDs: fileInfo?.containerIDs || [],
        };
      }),
    [items, logFileInfo],
  );

  const numItems = data.length;
  const maxDisplayRows = workloadKindFilter === kind ? numItems : 5;
  const [showAll, setShowAll] = useState(false);

  const [sorting, setSorting] = useState<SortingState>([{ id: 'name', desc: false }]);

  const [selectAll, setSelectAll] = useState(false);
  const [isChecked, setIsChecked] = useState<Map<string, boolean>>(new Map());

  const handleSelectAllChange = useCallback(() => {
    const newValue = !selectAll;
    setSelectAll(newValue);

    // update individual checkboxes
    const newIsChecked = new Map(isChecked);
    itemIDs.forEach((itemID) => newIsChecked.set(itemID, newValue));
    setIsChecked(newIsChecked);
  }, [itemIDs, selectAll, setSelectAll, isChecked, setIsChecked]);

  const handleSingleCheckboxChange = useCallback(
    (id: string) => {
      // update individual
      const newValue = !isChecked.get(id);
      const newIsChecked = new Map(isChecked);
      newIsChecked.set(id, newValue);
      setIsChecked(newIsChecked);

      // update selectAll based on all current items
      const allItemsChecked = itemIDs.every((itemID) => newIsChecked.get(itemID) || false);
      const someItemsUnchecked = itemIDs.some((itemID) => !newIsChecked.get(itemID));

      if (allItemsChecked) setSelectAll(true);
      if (someItemsUnchecked) setSelectAll(false);
    },
    [itemIDs, isChecked, setSelectAll, setIsChecked],
  );

  const meta = useMemo(
    () =>
      ({
        kubeContext,
        selectAll,
        isChecked,
        handleSelectAllChange,
        handleSingleCheckboxChange,
      }) satisfies WorkloadTableMeta,
    [kubeContext, selectAll, isChecked, handleSelectAllChange, handleSingleCheckboxChange],
  );

  const tableCfg = useMemo(
    () =>
      ({
        data,
        columns: WORKLOAD_TABLE_COLUMNS,
        meta,
        state: {
          sorting,
          columnVisibility: {
            size: isClusterAPIEnabled,
            lastModifiedAt: isClusterAPIEnabled,
          },
          pagination: {
            pageIndex: 0,
            pageSize: showAll ? numItems : maxDisplayRows,
          },
        },
        enableSortingRemoval: false,
        onSortingChange: setSorting,
        getCoreRowModel: getCoreRowModel(),
        getSortedRowModel: getSortedRowModel(),
        getPaginationRowModel: getPaginationRowModel(),
        manualPagination: false,
      }) satisfies TableOptions<WorkloadTableData>,
    [data, meta, sorting, isClusterAPIEnabled, showAll, numItems, maxDisplayRows, setSorting],
  );

  const table = useReactTable(tableCfg as TableOptions<WorkloadTableData>);

  // For label
  const Icon = KNOCKOUT_ICON_MAP[kind];
  const label = PLURAL_LABEL_MAP[kind];

  return (
    <>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead colSpan={5} className="px-0 pb-[5px] text-[0.9rem]">
            <div className="flex items-center">
              <Icon className="w-[24px] h-[24px] mr-[4px] text-primary" />
              <div className="font-medium mr-[8px] leading-none">{label}</div>
              {isFetching ? (
                <div>
                  <Spinner size="xs" />
                </div>
              ) : (
                <div className="px-[8px] py-[1px] bg-transparent border border-input font-semibold rounded-full text-xs">
                  {items && `${items?.length}`}
                </div>
              )}
            </div>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableHeader className="rounded-thead">
        {table.getHeaderGroups().map((headerGroup) => (
          <TableRow key={headerGroup.id}>
            {headerGroup.headers.map((header) => {
              const canSort = header.column.getCanSort();
              return (
                <TableHead key={header.id} onClick={canSort ? header.column.getToggleSortingHandler() : undefined}>
                  <div className={cn('flex group', canSort && 'cursor-pointer')}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                    {canSort && (
                      <SortIcon dir={header.column.getIsSorted()} descFirst={header.column.columnDef.sortDescFirst} />
                    )}
                  </div>
                </TableHead>
              );
            })}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody className="rounded-tbody [&_td]:bg-background">
        {numItems === 0 ? (
          <TableRow>
            <TableCell colSpan={table.getVisibleLeafColumns().length}>
              <div className="flex flex-col items-center  py-1 ">
                <Layers3 className="h-5 w-5 text-[currentColor]/25" />
                <span className="text-[currentColor]/25 italic font-medium">No resources found</span>
              </div>
            </TableCell>
          </TableRow>
        ) : (
          <>
            {table.getRowModel().rows.map((row) => (
              <MemoizedDataTableRow key={row.id} row={row} />
            ))}
          </>
        )}
      </TableBody>
      <TableBody>
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={table.getVisibleLeafColumns().length} className="pb-[30px]">
            {numItems > maxDisplayRows && (
              <button
                type="button"
                className="text-[currentColor]/50 hover:text-[currentColor]/70 text-sm font-medium cursor-pointer"
                onClick={() => setShowAll(!showAll)}
              >
                {showAll ? 'Show less...' : `Show ${numItems - maxDisplayRows} more...`}
              </button>
            )}
          </TableCell>
        </TableRow>
      </TableBody>
    </>
  );
});

DisplayWorkloadItems.displayName = 'DisplayWorkloadItems';

/**
 * DisplayWorkloads component
 */

const DisplayWorkloads = () => {
  const { kubeContext, workloadKindFilter } = useContext(Context);
  const searchQuery = useAtomValue(searchQueryAtom);
  const isLoading = useAtomValue(isLoadingAtomFamily(kubeContext));
  const totalCount = useAtomValue(filteredTotalCountAtomFamily(kubeContext));

  if (isLoading) return <div>Loading...</div>;

  // If loading & fetching is finished and there are no search results, display "No Results" UI
  if (searchQuery.trim() !== '' && totalCount === 0) {
    return (
      <div className="flex items-center border border-dashed border-secondary rounded-md justify-center h-32">
        <div className="text-center">
          <Search className="h-8 w-8 text-muted-foreground mx-auto mb-2" />
          <p className="text-base text-muted-foreground">No matching workloads found</p>
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-table-wrapper overflow-visible w-full">
      <Table>
        {ALL_WORKLOAD_KINDS.map((kind) => {
          if (!workloadKindFilter || workloadKindFilter === kind) {
            return <DisplayWorkloadItems key={kind} kind={kind} />;
          }
          return null;
        })}
      </Table>
    </div>
  );
};

/**
 * WorkloadCount component
 */

const WorkloadCount = ({ kind }: { kind: WorkloadKind }) => {
  const { kubeContext } = useContext(Context);
  const isFetching = useAtomValue(workloadIsFetchingAtomFamilies[kind](kubeContext));
  const count = useAtomValue(filteredWorkloadCountAtomFamilies[kind](kubeContext));
  return <span>{isFetching ? '-' : count}</span>;
};

/**
 * Sidebar component
 */

const Sidebar = () => {
  const { workloadKindFilter, setWorkloadKindFilter } = useContext(Context);

  return (
    <>
      <button type="button" className="cursor-pointer mb-[6px]" onClick={() => setWorkloadKindFilter(undefined)}>
        <span className="text-md text-muted-foreground">Workloads</span>
      </button>
      <ul className="space-y-[6px]">
        {ALL_WORKLOAD_KINDS.map((kind) => {
          const Icon = GLYPH_ICON_MAP[kind];
          return (
            <li className="group" key={kind}>
              <button
                type="button"
                className={cn(
                  'group flex items-center justify-between h-[40px] px-[8px] rounded-sm hover:bg-accent/45 w-full',
                  kind === workloadKindFilter && 'bg-accent!',
                )}
                onClick={() => setWorkloadKindFilter((w) => (w === kind ? undefined : kind))}
              >
                <div className="flex items-center gap-2">
                  <Icon className="w-[20px] h-[20px] text-[currentColor]" />
                  <span className="text-md">{PLURAL_LABEL_MAP[kind]}</span>
                </div>
                <div
                  className={cn(
                    'text-xs font-medium border border-input not-dark:group-has-hover:border-zinc-400/70 dark:group-has-hover:border-zinc-400 min-w-[24px] h-[24px] px-[4px] rounded-sm flex items-center justify-center',
                    kind === workloadKindFilter && 'border-zinc-400/70 dark:border-zinc-400',
                  )}
                >
                  <WorkloadCount kind={kind} />
                </div>
              </button>
            </li>
          );
        })}
      </ul>
    </>
  );
};

/**
 * NamespacesPicker component
 */

const NamespacesPicker = () => {
  const { kubeContext } = useContext(Context);
  const [namespaceFilter, setNamespaceFilter] = useAtom(namespaceFilterAtom);

  const { loading, data } = useListQueryWithSubscription({
    query: HOME_NAMESPACES_LIST_FETCH,
    subscription: HOME_NAMESPACES_LIST_WATCH,
    queryDataKey: 'coreV1NamespacesList',
    subscriptionDataKey: 'coreV1NamespacesWatch',
    variables: { kubeContext },
  });

  const ALL_NAMESPACES = '*';

  // Reset namespaces when kubeContext changes
  useEffect(() => {
    setNamespaceFilter('');
  }, [kubeContext]);

  return (
    <Select
      value={namespaceFilter === '' ? ALL_NAMESPACES : namespaceFilter}
      onValueChange={(v) => setNamespaceFilter(v === ALL_NAMESPACES ? '' : v)}
      disabled={loading}
    >
      <SelectTrigger className="w-[200px] bg-background">
        <SelectValue placeholder="Loading..." />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Namespaces</SelectLabel>
          <SelectItem value={ALL_NAMESPACES}>All namespaces</SelectItem>
          {data?.coreV1NamespacesList?.items.map((item) => (
            <SelectItem key={item.id} value={item.metadata.name}>
              {item.metadata.name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
};

/**
 * Main component
 */

const Main = () => {
  const { kubeContext } = useContext(Context);

  const readyWait = useSubscription(KUBERNETES_API_READY_WAIT, {
    skip: kubeContext === null,
    variables: { kubeContext },
  });

  const isLoading = useAtomValue(isLoadingAtomFamily(kubeContext));

  const [searchInputValue, setSearchInputValue] = useState('');
  const setSearchQuery = useSetAtom(searchQueryAtom);
  const debouncedSearch = useDebounceCallback((value: string) => setSearchQuery(value), 100);

  return (
    <div className="inline-block min-w-full px-[24px] py-[10px]">
      {readyWait.loading || kubeContext === null ? (
        <div>Connecting...</div>
      ) : (
        <form method="get" target="_blank" action={joinPaths(basename, '/console')}>
          <input type="hidden" name="kubeContext" value={kubeContext} />
          <div className="flex gap-4 pt-[14px] pb-[24px] justify-between flex-row">
            <div className="text-heading-2xl">Dashboard</div>
            <div className="flex gap-2">
              <SearchBox
                className="w-50 bg-background"
                value={searchInputValue}
                placeholder="Search workloads..."
                onChange={(e) => {
                  setSearchInputValue(e.target.value);
                  debouncedSearch(e.target.value);
                }}
                onKeyDown={(e) => e.key === 'Enter' && e.preventDefault()}
                disabled={isLoading}
              />
              <div className="block w-[200px]">
                <NamespacesPicker />
              </div>
              <Button type="submit">
                View in Console
                <ExternalLink className="w-[18px] h-[18px] ml-1" />
              </Button>
            </div>
          </div>
          <DisplayWorkloads />
        </form>
      )}
    </div>
  );
};

/**
 * InnerLayout component
 */

const InnerLayout = () => {
  const { sidebarOpen, setSidebarOpen, kubeContext, setKubeContext } = useContext(Context);

  const sidebarWidth = sidebarOpen ? 240 : 30;

  return (
    <div className="h-full flex flex-col">
      <div className="flex-1 h-0">
        <div className="flex h-full">
          <aside
            className={cn(
              'shrink-0 bg-strong border-r-1 transition-all duration-100 ease-in relative overflow-y-auto',
              'h-full flex flex-col justify-between',
              sidebarOpen ? 'px-[12px]' : 'px-[2px]',
            )}
            style={{ width: `${sidebarWidth}px` }}
          >
            <div>
              <header className="my-[24px] h-[36px] flex flex-row justify-between items-center gap-2">
                {sidebarOpen ? (
                  <>
                    <KubetailLogo className="text-primary h-full w-auto" />
                    <PanelLeftClose
                      className="h-[20px] cursor-pointer text-muted-foreground hover:text-primary "
                      onClick={() => setSidebarOpen(false)}
                    />
                  </>
                ) : (
                  <PanelLeftOpen
                    className="h-[20px] cursor-pointer text-muted-foreground hover:text-primary "
                    onClick={() => setSidebarOpen(true)}
                  />
                )}
              </header>
              {sidebarOpen && (
                <>
                  <div className="my-[12px]">
                    {appConfig.environment === 'desktop' && (
                      <KubeContextPicker className="my-[24px] w-full" value={kubeContext} setValue={setKubeContext} />
                    )}
                  </div>
                  <Sidebar />
                </>
              )}
            </div>
            {sidebarOpen && <SettingsDropdown />}
          </aside>
          <main className="flex-1 overflow-auto bg-muted">
            <Main />
          </main>
        </div>
      </div>
    </div>
  );
};

/**
 * Page component
 */

export default function Page() {
  const [kubeContext, setKubeContext] = useState(defaultKubeContext);
  const [workloadKindFilter, setWorkloadKindFilter] = useState<WorkloadKind>();
  const [sidebarOpen, setSidebarOpen] = useState(true);

  const context = useMemo(
    () => ({
      kubeContext,
      setKubeContext,
      workloadKindFilter,
      setWorkloadKindFilter,
      sidebarOpen,
      setSidebarOpen,
    }),
    [kubeContext, setKubeContext, workloadKindFilter, setWorkloadKindFilter, sidebarOpen, setSidebarOpen],
  );

  return (
    <AuthRequired>
      <Context.Provider value={context}>
        <WorkloadDataProvider kubeContext={kubeContext} />
        <LogMetadataProvider kubeContext={kubeContext} />
        <AppLayout>
          <InnerLayout />
        </AppLayout>
      </Context.Provider>
    </AuthRequired>
  );
}

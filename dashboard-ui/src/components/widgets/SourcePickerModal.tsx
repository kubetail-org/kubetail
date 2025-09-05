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

import type { CheckedState } from '@radix-ui/react-checkbox';
import { flexRender, getCoreRowModel, getSortedRowModel, useReactTable } from '@tanstack/react-table';
import type { Cell, ColumnDef, Row, SortDirection, SortingState, TableMeta, TableOptions } from '@tanstack/react-table';
import { ChevronDown, ChevronUp } from 'lucide-react';
import { createContext, useCallback, useContext, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@kubetail/ui/elements/table';

import Modal from '@/components/elements/Modal';
import AdaptiveTimeAgo from '@/components/widgets/AdaptiveTimeAgo';

import type {
  SourcePickerCronJobsListFetchQuery,
  SourcePickerDaemonSetsListFetchQuery,
  SourcePickerDeploymentsListFetchQuery,
  SourcePickerGenericListItemFragmentFragment,
  SourcePickerJobsListFetchQuery,
  SourcePickerPodsListFetchQuery,
  SourcePickerReplicaSetsListFetchQuery,
  SourcePickerStatefulSetsListFetchQuery,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import {
  SOURCE_PICKER_CRONJOBS_LIST_FETCH,
  SOURCE_PICKER_CRONJOBS_LIST_WATCH,
  SOURCE_PICKER_DAEMONSETS_LIST_FETCH,
  SOURCE_PICKER_DAEMONSETS_LIST_WATCH,
  SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH,
  SOURCE_PICKER_DEPLOYMENTS_LIST_WATCH,
  SOURCE_PICKER_JOBS_LIST_FETCH,
  SOURCE_PICKER_JOBS_LIST_WATCH,
  SOURCE_PICKER_PODS_LIST_FETCH,
  SOURCE_PICKER_PODS_LIST_WATCH,
  SOURCE_PICKER_REPLICASETS_LIST_FETCH,
  SOURCE_PICKER_REPLICASETS_LIST_WATCH,
  SOURCE_PICKER_STATEFULSETS_LIST_FETCH,
  SOURCE_PICKER_STATEFULSETS_LIST_WATCH,
  SOURCE_PICKER_NAMESPACES_LIST_FETCH,
  SOURCE_PICKER_NAMESPACES_LIST_WATCH,
} from '@/lib/graphql/dashboard/ops';
import { useListQueryWithSubscription, useWorkloadCounter } from '@/lib/hooks';
import { ALL_WORKLOAD_KINDS, KNOCKOUT_ICON_MAP, PLURAL_LABEL_MAP, WorkloadKind } from '@/lib/workload';
import { cn } from '@/lib/util';

/**
 * Shared variables and types
 */

type ContextType = {
  kubeContext: string;
  namespaceFilter: string;
  setNamespaceFilter: React.Dispatch<string>;
  selectedSources: Set<string>;
  setSelectedSources: React.Dispatch<React.SetStateAction<Set<string>>>;
};

const Context = createContext({} as ContextType);

/**
 * Sidebar component
 */

type SidebarProps = {
  workloadState: [WorkloadKind | null, React.Dispatch<React.SetStateAction<WorkloadKind | null>>];
};

const Sidebar = ({ workloadState }: SidebarProps) => {
  const { kubeContext, namespaceFilter } = useContext(Context);
  const { counter } = useWorkloadCounter(kubeContext, namespaceFilter);
  const [currWorkload, setCurrWorkload] = workloadState;

  return (
    <ul className="text-[.85rem]">
      <li>
        <div className="font-bold text-chrome-600 mt-[5px] mb-[12px]">Workloads</div>
        <div>
          <ul className="inline-grid space-y-0">
            {ALL_WORKLOAD_KINDS.map((kind) => {
              const Icon = KNOCKOUT_ICON_MAP[kind];
              return (
                <li key={kind} className="ml-[-8px]">
                  <button
                    type="button"
                    className={cn(
                      'w-full px-[8px] py-[5px] cursor-pointer rounded-xs flex items-center',
                      currWorkload === kind ? 'bg-chrome-300' : 'hover:bg-chrome-200',
                    )}
                    onClick={() => setCurrWorkload(kind)}
                  >
                    <Icon className="h-[18px] w-[18px] text-primary" />
                    <div className="ml-1 text-chrome-700">
                      {PLURAL_LABEL_MAP[kind]} {counter.has(kind) && `(${counter.get(kind)})`}
                    </div>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      </li>
    </ul>
  );
};

/**
 * CheckboxHeaderCell component
 */

type CheckboxHeaderCellProps = {
  sourceStrings: Set<string>;
};

const CheckboxHeaderCell = ({ sourceStrings }: CheckboxHeaderCellProps) => {
  const { selectedSources, setSelectedSources } = useContext(Context);

  const checkedCount = useMemo(
    () => Array.from(sourceStrings).filter((sourceString) => selectedSources.has(sourceString)).length,
    [sourceStrings, selectedSources],
  );

  const checkboxState = useMemo(() => {
    if (sourceStrings.size === 0) return false;
    if (checkedCount === 0) return false;
    if (checkedCount === sourceStrings.size) return true;
    return 'indeterminate';
  }, [sourceStrings.size, checkedCount]);

  const handleCheckedChange = useCallback(
    (checked: CheckedState) => {
      setSelectedSources((sources) => {
        const newSources = new Set(sources);
        if (checked) {
          sourceStrings.forEach((sourceString) => newSources.add(sourceString));
        } else {
          sourceStrings.forEach((sourceString) => newSources.delete(sourceString));
        }
        return newSources;
      });
    },
    [sourceStrings, setSelectedSources],
  );

  return (
    <div className="flex items-center">
      <Checkbox checked={checkboxState} onCheckedChange={handleCheckedChange} />
    </div>
  );
};

/**
 * CheckboxBodyCell component
 */

type CheckboxBodyCellProps = {
  id: string;
  sourceString: string;
};

const CheckboxBodyCell = ({ id, sourceString }: CheckboxBodyCellProps) => {
  const { selectedSources, setSelectedSources } = useContext(Context);

  const handleCheckedChange = useCallback(
    (checked: CheckedState) => {
      setSelectedSources((sources) => {
        const newSources = new Set(sources);
        if (checked) {
          newSources.add(sourceString);
        } else {
          newSources.delete(sourceString);
        }
        return newSources;
      });
    },
    [sourceString, setSelectedSources],
  );

  return (
    <div className="flex items-center">
      <Checkbox data-id={id} checked={selectedSources.has(sourceString)} onCheckedChange={handleCheckedChange} />
    </div>
  );
};

/**
 * DisplayWorkloadItems component
 */

type WorkloadTableData = {
  id: string;
  name: string;
  namespace: string;
  createdAt: Date;
};

interface WorkloadTableMeta extends TableMeta<WorkloadTableData> {
  kind: WorkloadKind;
  sourceStrings: Set<string>;
}

const WORKLOAD_TABLE_COLUMNS = [
  {
    id: 'checkbox',
    header: ({ table }) => {
      const meta = table.options.meta as WorkloadTableMeta;
      return <CheckboxHeaderCell sourceStrings={meta.sourceStrings} />;
    },
    cell: ({ row, table }) => {
      const { id, namespace, name } = row.original;
      const meta = table.options.meta as WorkloadTableMeta;
      const sourceString = `${namespace}:${meta.kind}/${name}/*`;
      return <CheckboxBodyCell id={id} sourceString={sourceString} />;
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
] satisfies ColumnDef<WorkloadTableData>[];

type SortIconProps = {
  dir: SortDirection | false;
  descFirst: boolean | undefined;
};

const SortIcon = ({ dir, descFirst }: SortIconProps) => {
  const iconCN = 'h-5 w-5 ml-2 flex-none text-chrome-400 ';

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

const DataTableCell = ({ cell }: { cell: Cell<WorkloadTableData, unknown> }) => (
  <TableCell>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
);

const DataTableRow = ({ row }: { row: Row<WorkloadTableData> }) => (
  <TableRow>
    {row.getVisibleCells().map((cell) => (
      <DataTableCell key={cell.id} cell={cell} />
    ))}
  </TableRow>
);

type WorkloadItem = SourcePickerGenericListItemFragmentFragment;

type DisplayWorkloadItemsProps = {
  kind: WorkloadKind;
  items: WorkloadItem[];
};

const DisplayWorkloadItems = ({ kind, items }: DisplayWorkloadItemsProps) => {
  const { namespaceFilter } = useContext(Context);

  const filterFn = useCallback(
    (item: WorkloadItem) => {
      // Remove deleted items
      if (item.metadata.deletionTimestamp) return false;

      // Apply namespace filter
      if (namespaceFilter !== '' && item.metadata.namespace !== namespaceFilter) return false;

      return true;
    },
    [namespaceFilter],
  );

  const data = useMemo(
    () =>
      items.filter(filterFn).map((item) => ({
        id: item.id,
        name: item.metadata.name,
        namespace: item.metadata.namespace,
        createdAt: item.metadata.creationTimestamp,
      })),
    [filterFn, JSON.stringify(items)],
  );

  const [sorting, setSorting] = useState<SortingState>([{ id: 'name', desc: false }]);

  const meta = useMemo(
    () =>
      ({
        kind,
        sourceStrings: new Set(data.map((item) => `${item.namespace}:${kind}/${item.name}/*`)),
      }) satisfies WorkloadTableMeta,
    [kind, data],
  );

  const tableCfg = useMemo(
    () => ({
      data,
      columns: WORKLOAD_TABLE_COLUMNS,
      meta,
      state: { sorting },
      onSortingChange: setSorting,
      getCoreRowModel: getCoreRowModel(),
      getSortedRowModel: getSortedRowModel(),
    }),
    [data, meta, sorting, setSorting],
  );

  const table = useReactTable(tableCfg as TableOptions<WorkloadTableData>);

  return (
    <Table containerClassName="overflow-x-hidden overflow-y-auto shadow ring ring-black/5 rounded-lg h-full">
      <TableHeader className="bg-chrome-50 sticky top-0">
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
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <DataTableRow key={row.id} row={row} />
        ))}
      </TableBody>
    </Table>
  );
};

/**
 * DisplayWorkload component
 */

const workloadQueryConfig = {
  [WorkloadKind.CRONJOBS]: {
    query: SOURCE_PICKER_CRONJOBS_LIST_FETCH,
    subscription: SOURCE_PICKER_CRONJOBS_LIST_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    getItems: (data: SourcePickerCronJobsListFetchQuery) => data?.batchV1CronJobsList?.items,
  },
  [WorkloadKind.DAEMONSETS]: {
    query: SOURCE_PICKER_DAEMONSETS_LIST_FETCH,
    subscription: SOURCE_PICKER_DAEMONSETS_LIST_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    getItems: (data: SourcePickerDaemonSetsListFetchQuery) => data?.appsV1DaemonSetsList?.items,
  },
  [WorkloadKind.DEPLOYMENTS]: {
    query: SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH,
    subscription: SOURCE_PICKER_DEPLOYMENTS_LIST_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    getItems: (data: SourcePickerDeploymentsListFetchQuery) => data?.appsV1DeploymentsList?.items,
  },
  [WorkloadKind.JOBS]: {
    query: SOURCE_PICKER_JOBS_LIST_FETCH,
    subscription: SOURCE_PICKER_JOBS_LIST_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    getItems: (data: SourcePickerJobsListFetchQuery) => data?.batchV1JobsList?.items,
  },
  [WorkloadKind.PODS]: {
    query: SOURCE_PICKER_PODS_LIST_FETCH,
    subscription: SOURCE_PICKER_PODS_LIST_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    getItems: (data: SourcePickerPodsListFetchQuery) => data?.coreV1PodsList?.items,
  },
  [WorkloadKind.REPLICASETS]: {
    query: SOURCE_PICKER_REPLICASETS_LIST_FETCH,
    subscription: SOURCE_PICKER_REPLICASETS_LIST_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    getItems: (data: SourcePickerReplicaSetsListFetchQuery) => data?.appsV1ReplicaSetsList?.items,
  },
  [WorkloadKind.STATEFULSETS]: {
    query: SOURCE_PICKER_STATEFULSETS_LIST_FETCH,
    subscription: SOURCE_PICKER_STATEFULSETS_LIST_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    getItems: (data: SourcePickerStatefulSetsListFetchQuery) => data?.appsV1StatefulSetsList?.items,
  },
};

type DisplayWorkloadProps = {
  kind: WorkloadKind;
};

const DisplayWorkload = ({ kind }: DisplayWorkloadProps) => {
  const { kubeContext } = useContext(Context);

  const cfg = workloadQueryConfig[kind];
  const { loading, fetching, data } = useListQueryWithSubscription({
    query: cfg.query,
    subscription: cfg.subscription,
    // @ts-expect-error
    queryDataKey: cfg.queryDataKey,
    // @ts-expect-error
    subscriptionDataKey: cfg.subscriptionDataKey,
    variables: { kubeContext },
  });

  if (loading || fetching) return <Spinner size="sm" />;

  const items = (data && cfg.getItems(data)) ?? [];

  return <DisplayWorkloadItems kind={kind} items={items} />;
};

/**
 * Explorer component
 */

const Explorer = () => {
  const workloadState = useState<WorkloadKind | null>(null);
  const currWorkload = workloadState[0];

  return (
    <div className="flex space-x-2">
      <Sidebar workloadState={workloadState} />
      <div className="grow">
        <div className="h-[50vh] w-full">{currWorkload && <DisplayWorkload kind={currWorkload} />}</div>
      </div>
    </div>
  );
};

/**
 * NamespacePicker component
 */

const NamespacePicker = () => {
  const { kubeContext, namespaceFilter, setNamespaceFilter } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: SOURCE_PICKER_NAMESPACES_LIST_FETCH,
    subscription: SOURCE_PICKER_NAMESPACES_LIST_WATCH,
    queryDataKey: 'coreV1NamespacesList',
    subscriptionDataKey: 'coreV1NamespacesWatch',
    variables: { kubeContext },
  });

  const ALL_NAMESPACES = '*';

  return (
    <Select
      value={namespaceFilter === '' ? ALL_NAMESPACES : namespaceFilter}
      onValueChange={(value) => setNamespaceFilter(value === ALL_NAMESPACES ? '' : value)}
      disabled={loading}
    >
      <SelectTrigger className="h-[35px] bg-chrome-50 border border-chrome-30 text-sm rounded-lg mt-0!">
        <SelectValue placeholder="Loading..." />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL_NAMESPACES}>All namespaces</SelectItem>
        {data?.coreV1NamespacesList?.items.map((item) => (
          <SelectItem key={item.id} value={item.metadata.name}>
            {item.metadata.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
};

/**
 * SourcePickerModal component
 */

const SourcePickerModal = ({ onClose }: { onClose: (value?: boolean) => void }) => {
  const [searchParams] = useSearchParams();
  const [namespaceFilter, setNamespaceFilter] = useState('');
  const [selectedSources, setSelectedSources] = useState(new Set(searchParams.getAll('source')));

  const kubeContext = searchParams.get('kubeContext') || '';

  const handleUpdate = () => {
    const sourcePaths = Array.from(selectedSources);
    sourcePaths.sort();

    searchParams.delete('source');
    sourcePaths.forEach((sourcePath) => searchParams.append('source', sourcePath));

    // TODO: instead of navigating to new url can we use react-router?
    const currentUrl = new URL(window.location.href);
    currentUrl.search = new URLSearchParams(searchParams).toString();
    window.location.href = currentUrl.toString();

    onClose();
  };

  const context = useMemo(
    () => ({
      kubeContext,
      namespaceFilter,
      setNamespaceFilter,
      selectedSources,
      setSelectedSources,
    }),
    [kubeContext, namespaceFilter, setNamespaceFilter, selectedSources, setSelectedSources],
  );

  return (
    <Context.Provider value={context}>
      <Modal open onClose={() => onClose()} className="max-w-[1000px]!">
        <div className="flex items-center justify-between mb-[15px]">
          <div className="font-semibold">Choose logging sources</div>
          <div className="max-w-[200px]">
            <NamespacePicker />
          </div>
        </div>
        <Explorer />
        <div className="flex justify-end space-x-2 mt-[15px]">
          <Button variant="secondary" onClick={() => onClose()}>
            Cancel
          </Button>
          <Button onClick={() => handleUpdate()}>Update</Button>
        </div>
      </Modal>
    </Context.Provider>
  );
};

export default SourcePickerModal;

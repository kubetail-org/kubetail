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

import { HighlightedText } from '@/components/primitives/highlighted-text';
import { VisibleText } from '@/components/primitives/visible-text';
import { queryResultOf } from '@/lib/chats';
import type { QueryResult } from '@/lib/chats';

// A KubeQuery call's body: its SQL, highlighted, over the rows it read as a
// table and a footer counting them and naming the cluster. An answer that is not
// rows (an error, a withheld or saved result) draws as its text. The SQL is the
// model's and the cells are the cluster's, so both draw through VisibleText.
export function KubeQuery({ sql, output }: { sql: string; output: string }) {
  const result = queryResultOf(output);
  return (
    <div className="overflow-hidden rounded-md border border-border">
      <pre className="bg-muted px-3 py-2.5 font-mono leading-4.5 break-all whitespace-pre-wrap text-muted-foreground">
        <HighlightedText lang="sql" text={sql} />
      </pre>
      {result ? (
        <>
          {result.columns.length > 0 && <Rows result={result} />}
          <p className="border-t border-border bg-muted px-3 py-1.5 font-medium text-muted-foreground">
            <VisibleText text={footerOf(result)} />
          </p>
        </>
      ) : (
        output !== '' && (
          <pre className="border-t border-border px-3 py-2.5 font-mono leading-4.5 break-all whitespace-pre-wrap">
            {output}
          </pre>
        )
      )}
    </div>
  );
}

const CELL = 'p-1.5 first:pl-3 last:pr-3';

function Rows({ result: { columns, rows } }: { result: QueryResult }) {
  // A column of numbers reads right-aligned, as a count or a size does.
  const align = columns.map((_, c) => (isNumeric(rows.map((row) => row[c])) ? 'text-right' : 'text-left'));
  return (
    <div className="max-h-80 overflow-auto border-t border-border">
      <table className="w-full font-mono leading-4.5 whitespace-nowrap">
        <thead className="text-muted-foreground">
          <tr>
            {columns.map((name, c) => (
              // oxlint-disable-next-line react/no-array-index-key -- a result may name a column twice
              <th key={c} className={`${CELL} ${align[c]} font-normal`}>
                <VisibleText text={name} />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, r) => (
            // oxlint-disable-next-line react/no-array-index-key -- rows have no id and never reorder
            <tr key={r} className="border-t border-border">
              {row.map((value, c) => (
                // oxlint-disable-next-line react/no-array-index-key -- a row's cells are its columns, in order
                <td key={c} className={`${CELL} ${align[c]}`}>
                  {value === null ? (
                    <span className="text-muted-foreground">null</span>
                  ) : (
                    // A string as it is; a number or a nested value as JSON.
                    <VisibleText text={typeof value === 'string' ? value : JSON.stringify(value)} />
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// Whether a column holds numbers alone, nulls aside.
function isNumeric(cells: unknown[]): boolean {
  const values = cells.filter((v) => v !== null);
  return values.length > 0 && values.every((v) => typeof v === 'number');
}

function footerOf({ rows, more, cluster }: QueryResult): string {
  const n = rows.length;
  const count = more ? `First ${n} rows` : `${n} ${n === 1 ? 'row' : 'rows'}`;
  return cluster === '' ? count : `${count} · ${cluster}`;
}

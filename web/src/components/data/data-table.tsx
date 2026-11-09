import { useState, type ReactNode } from "react";
import {
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

interface DataTableProps<T> {
  // TanStack's accessor inference is value-specific; the table renders
  // heterogeneous columns, so the component must accept the erased form.
  columns: ColumnDef<T, any>[];
  data: T[];
  loading?: boolean;
  empty?: ReactNode;
  pageSize?: number;
  onRowClick?: (row: T) => void;
  stickyFirstColumn?: boolean;
  className?: string;
  pagination?: ServerPagination;
}

export interface ServerPagination {
  page: number;
  pageSize: number;
  total: number;
  totalKnown: boolean;
  hasMore: boolean;
  canPrevious?: boolean;
  onPrevious?: () => void;
  onNext?: () => void;
  warnings?: string[];
}

export function DataTable<T>({ columns, data, loading = false, empty, pageSize = 20, onRowClick, stickyFirstColumn = true, className, pagination }: DataTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const pageLocalSorting = Boolean(pagination && sorting.length > 0);
  const minimumWidth = Math.max(640, columns.reduce((width, column) => width + (typeof column.size === "number" ? Math.max(120, column.size) : 120), 0));
  const stickyColumnClass = "sticky left-0 z-20 w-36 min-w-36 max-w-36 overflow-hidden bg-muted shadow-[1px_0_0_hsl(var(--border))] md:static md:z-auto md:w-auto md:min-w-0 md:max-w-none md:overflow-visible md:bg-transparent md:shadow-none";
  const stickyCellClass = "sticky left-0 z-10 w-36 min-w-36 max-w-36 overflow-hidden bg-card shadow-[1px_0_0_hsl(var(--border))] md:static md:z-auto md:w-auto md:min-w-0 md:max-w-none md:overflow-visible md:bg-transparent md:shadow-none";
  const actionHeaderClass = "sticky right-0 z-20 w-14 min-w-14 max-w-14 bg-muted shadow-[-1px_0_0_hsl(var(--border))] md:static md:z-auto md:w-16 md:min-w-16 md:max-w-16 md:bg-transparent md:shadow-none";
  const actionCellClass = "sticky right-0 z-10 w-14 min-w-14 max-w-14 bg-card shadow-[-1px_0_0_hsl(var(--border))] md:static md:z-auto md:w-16 md:min-w-16 md:max-w-16 md:bg-transparent md:shadow-none";
  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: pagination ? undefined : getPaginationRowModel(),
    initialState: { pagination: { pageSize } },
  });

  if (loading) {
    return (
      <div className={cn("rounded-lg border bg-card", className)}>
        <div className="space-y-3 p-4">
          {Array.from({ length: 6 }).map((_, index) => <Skeleton key={index} className="h-9 w-full" />)}
        </div>
      </div>
    );
  }

  return (
    <div className={cn("overflow-hidden rounded-lg border bg-card", className)}>
      {pagination?.warnings?.length ? (
        <div className="flex items-start gap-2 border-b border-warning/30 bg-warning/10 px-4 py-2 text-xs text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{pagination.warnings.join("；")}</span>
        </div>
      ) : null}
      {pageLocalSorting ? (
        <div className="flex items-start gap-2 border-b bg-muted/30 px-4 py-2 text-xs text-muted-foreground">
          <ArrowUpDown className="mt-0.5 h-4 w-4 shrink-0" />
          <span>当前排序仅作用于本页；切换分页后会按服务端顺序展示。</span>
        </div>
      ) : null}
      <Table style={{ minWidth: `${minimumWidth}px` }}>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => {
                const sortable = header.column.getCanSort();
                const sorted = header.column.getIsSorted();
                const meta = header.column.columnDef.meta;
                return (
                  <TableHead
                    key={header.id}
                    aria-sort={sortable ? (sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none") : undefined}
                    style={{
                      width: meta?.width ?? (header.column.getSize() === 150 ? undefined : header.getSize()),
                      minWidth: meta?.minWidth,
                      maxWidth: meta?.maxWidth,
                    }}
                    className={cn(
                      header.index === 0 && stickyFirstColumn && stickyColumnClass,
                      header.column.id === "actions" && actionHeaderClass,
                      meta?.headerClassName,
                    )}
                  >
                    {header.isPlaceholder ? null : (
                      <button
                        type="button"
                        title={pagination ? "仅对当前页排序" : undefined}
                        className={cn("flex min-w-0 max-w-full items-center gap-1", sortable ? "cursor-pointer hover:text-foreground" : "cursor-default")}
                        onClick={sortable ? header.column.getToggleSortingHandler() : undefined}
                      >
                        <span className="truncate">{flexRender(header.column.columnDef.header, header.getContext())}</span>
                        {sortable ? sorted === "asc" ? <ArrowUp className="h-3 w-3 shrink-0" /> : sorted === "desc" ? <ArrowDown className="h-3 w-3 shrink-0" /> : <ArrowUpDown className="h-3 w-3 shrink-0 opacity-40" /> : null}
                      </button>
                    )}
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.length ? table.getRowModel().rows.map((row) => (
            <TableRow
              key={row.id}
              className={onRowClick ? "cursor-pointer" : undefined}
              onClick={onRowClick ? (event) => {
                if ((event.target as HTMLElement).closest("[data-row-action]")) return;
                onRowClick(row.original);
              } : undefined}
              tabIndex={onRowClick ? 0 : undefined}
              onKeyDown={onRowClick ? (event) => {
                if (event.key === "Enter" && !(event.target as HTMLElement).closest("[data-row-action]")) onRowClick(row.original);
              } : undefined}
            >
              {row.getVisibleCells().map((cell, index) => (
                <TableCell
                  key={cell.id}
                  style={{
                    width: cell.column.columnDef.meta?.width ?? (cell.column.getSize() === 150 ? undefined : cell.column.getSize()),
                    minWidth: cell.column.columnDef.meta?.minWidth,
                    maxWidth: cell.column.columnDef.meta?.maxWidth,
                  }}
                  className={cn(
                    index === 0 && stickyFirstColumn && stickyCellClass,
                    cell.column.id === "actions" && actionCellClass,
                    cell.column.columnDef.meta?.cellClassName,
                  )}
                >
                  {index === 0 && stickyFirstColumn
                    ? <div className="w-full truncate">{flexRender(cell.column.columnDef.cell, cell.getContext())}</div>
                    : flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          )) : <TableEmpty colSpan={columns.length}>{empty ?? "暂无数据"}</TableEmpty>}
        </TableBody>
      </Table>
      {pagination ? (pagination.page > 1 || pagination.hasMore || (pagination.totalKnown && pagination.total > pagination.pageSize)) ? (
        <div className="flex items-center justify-between gap-3 border-t bg-card px-4 py-3 text-xs text-muted-foreground">
          <span aria-live="polite">
            {pagination.totalKnown
              ? `共 ${pagination.total} 条 · 第 ${pagination.page} / ${Math.max(1, Math.ceil(pagination.total / pagination.pageSize))} 页`
              : `已加载 ${data.length} 条 · 第 ${pagination.page} 页${pagination.hasMore ? " · 还有更多" : ""}`}
          </span>
          <div className="flex items-center gap-1">
            <Button variant="outline" size="icon-sm" aria-label="上一页" onClick={pagination.onPrevious} disabled={!(pagination.canPrevious ?? pagination.page > 1)}><ChevronLeft className="h-4 w-4" /></Button>
            <Button variant="outline" size="icon-sm" aria-label="下一页" onClick={pagination.onNext} disabled={!pagination.hasMore}><ChevronRight className="h-4 w-4" /></Button>
          </div>
        </div>
      ) : null : table.getPageCount() > 1 ? (
        <div className="flex items-center justify-between border-t bg-card px-4 py-3 text-xs text-muted-foreground">
          <span aria-live="polite">共 {data.length} 条 · 第 {table.getState().pagination.pageIndex + 1} / {table.getPageCount()} 页</span>
          <div className="flex items-center gap-1">
            <Button variant="outline" size="icon-sm" aria-label="上一页" onClick={() => table.previousPage()} disabled={!table.getCanPreviousPage()}><ChevronLeft className="h-4 w-4" /></Button>
            <Button variant="outline" size="icon-sm" aria-label="下一页" onClick={() => table.nextPage()} disabled={!table.getCanNextPage()}><ChevronRight className="h-4 w-4" /></Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}


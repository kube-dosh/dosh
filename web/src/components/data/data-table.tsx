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
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight } from "lucide-react";
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
  className?: string;
}

export function DataTable<T>({ columns, data, loading = false, empty, pageSize = 20, onRowClick, className }: DataTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const minimumWidth = Math.max(640, columns.length * 120);
  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
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
      <Table style={{ minWidth: `${minimumWidth}px` }}>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => {
                const sortable = header.column.getCanSort();
                const sorted = header.column.getIsSorted();
                return (
                  <TableHead
                    key={header.id}
                    style={{ width: header.column.getSize() === 150 ? undefined : header.getSize() }}
                    className={cn(header.index === 0 && "sticky left-0 z-20 min-w-44 bg-muted shadow-[1px_0_0_hsl(var(--border))] md:static md:z-auto md:min-w-0 md:bg-transparent md:shadow-none")}
                  >
                    {header.isPlaceholder ? null : (
                      <button
                        type="button"
                        className={cn("inline-flex items-center gap-1", sortable ? "cursor-pointer hover:text-foreground" : "cursor-default")}
                        onClick={sortable ? header.column.getToggleSortingHandler() : undefined}
                      >
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {sortable ? sorted === "asc" ? <ArrowUp className="h-3 w-3" /> : sorted === "desc" ? <ArrowDown className="h-3 w-3" /> : <ArrowUpDown className="h-3 w-3 opacity-40" /> : null}
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
              onClick={onRowClick ? () => onRowClick(row.original) : undefined}
              tabIndex={onRowClick ? 0 : undefined}
              onKeyDown={onRowClick ? (event) => { if (event.key === "Enter") onRowClick(row.original); } : undefined}
            >
              {row.getVisibleCells().map((cell, index) => (
                <TableCell key={cell.id} className={cn(index === 0 && "sticky left-0 z-10 min-w-44 bg-card shadow-[1px_0_0_hsl(var(--border))] md:static md:z-auto md:min-w-0 md:bg-transparent md:shadow-none")}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          )) : <TableEmpty colSpan={columns.length}>{empty ?? "暂无数据"}</TableEmpty>}
        </TableBody>
      </Table>
      {table.getPageCount() > 1 ? (
        <div className="flex items-center justify-between border-t bg-card px-4 py-3 text-xs text-muted-foreground">
          <span>共 {data.length} 条 · 第 {table.getState().pagination.pageIndex + 1} / {table.getPageCount()} 页</span>
          <div className="flex items-center gap-1">
            <Button variant="outline" size="icon-sm" onClick={() => table.previousPage()} disabled={!table.getCanPreviousPage()}><ChevronLeft className="h-4 w-4" /></Button>
            <Button variant="outline" size="icon-sm" onClick={() => table.nextPage()} disabled={!table.getCanNextPage()}><ChevronRight className="h-4 w-4" /></Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}


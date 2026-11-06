import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

describe("Table", () => {
  it("exposes the scroll container to keyboard users", () => {
    render(
      <Table>
        <TableHeader><TableRow><TableHead>key</TableHead></TableRow></TableHeader>
        <TableBody>
          <TableRow><TableCell>value</TableCell></TableRow>
        </TableBody>
      </Table>,
    );

    const region = screen.getByRole("region", { name: "可滚动表格" });
    expect(region).toHaveAttribute("tabindex", "0");
    expect(region).toHaveClass("focus-visible:ring-2");
    expect(region).toHaveClass("overflow-auto", "overscroll-x-contain", "scrollbar-thin");
    expect(region).not.toHaveClass("touch-pan-x");
    expect(screen.getByRole("table")).toHaveClass("min-w-[640px]");
    expect(screen.getByRole("columnheader")).toHaveClass("whitespace-nowrap");
    expect(screen.getByRole("cell")).toHaveClass("whitespace-nowrap");
    expect(screen.getByText("左右滑动查看完整列")).toBeInTheDocument();
  });
});

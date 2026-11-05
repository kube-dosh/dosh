import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";

describe("Table", () => {
  it("exposes the scroll container to keyboard users", () => {
    render(
      <Table>
        <TableBody>
          <TableRow><TableCell>value</TableCell></TableRow>
        </TableBody>
      </Table>,
    );

    const region = screen.getByRole("region", { name: "可滚动表格" });
    expect(region).toHaveAttribute("tabindex", "0");
    expect(region).toHaveClass("focus-visible:ring-2");
  });
});

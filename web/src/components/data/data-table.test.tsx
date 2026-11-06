import { render, screen } from "@testing-library/react";
import { createColumnHelper } from "@tanstack/react-table";
import { describe, expect, it } from "vitest";
import { DataTable } from "@/components/data/data-table";

interface Row {
  name: string;
  namespace: string;
  status: string;
  node: string;
  age: string;
  image: string;
  restarts: string;
}

const column = createColumnHelper<Row>();
const row: Row = {
  name: "web-abcdef-12345",
  namespace: "default",
  status: "Running",
  node: "worker-0001",
  age: "3d",
  image: "registry.example.test/web:latest",
  restarts: "0",
};

describe("DataTable", () => {
  it("keeps a readable mobile minimum width and pins the identity column", () => {
    const columns = [
      column.accessor("name", { header: "名称" }),
      column.accessor("namespace", { header: "命名空间" }),
      column.accessor("status", { header: "状态" }),
      column.accessor("node", { header: "节点" }),
      column.accessor("age", { header: "年龄" }),
      column.accessor("image", { header: "镜像" }),
      column.accessor("restarts", { header: "重启" }),
    ];

    render(<DataTable columns={columns} data={[row]} />);

    expect(screen.getByRole("table")).toHaveStyle({ minWidth: "840px" });
    expect(screen.getByRole("columnheader", { name: "名称" })).toHaveClass("sticky", "left-0", "w-36", "max-w-36");
    expect(screen.getByRole("cell", { name: row.name })).toHaveClass("sticky", "left-0", "w-36", "max-w-36", "bg-card");
    expect(screen.getByText(row.name)).toHaveClass("truncate");
    expect(screen.getByText("左右滑动查看完整列")).toBeInTheDocument();
  });

  it("uses the 640px floor for compact tables", () => {
    const columns = [
      column.accessor("name", { header: "名称" }),
      column.accessor("status", { header: "状态" }),
    ];

    render(<DataTable columns={columns} data={[row]} />);

    expect(screen.getByRole("table")).toHaveStyle({ minWidth: "640px" });
  });

  it("reserves the configured width of wide columns", () => {
    const columns = [
      column.accessor("name", { header: "名称" }),
      column.accessor("status", { header: "状态" }),
      column.accessor("image", { header: "消息", size: 420 }),
    ];

    render(<DataTable columns={columns} data={[row]} />);

    expect(screen.getByRole("table")).toHaveStyle({ minWidth: "660px" });
    expect(screen.getByRole("columnheader", { name: "消息" })).toHaveStyle({ width: "420px" });
    expect(screen.getByRole("cell", { name: row.image })).toHaveStyle({ width: "420px" });
  });

  it("applies per-column responsive width and wrapping metadata", () => {
    const columns = [
      column.accessor("name", { header: "名称" }),
      column.accessor("image", {
        header: "消息",
        size: 420,
        meta: { width: "48%", minWidth: 420, maxWidth: 760, headerClassName: "message-header", cellClassName: "message-cell" },
      }),
    ];

    render(<DataTable columns={columns} data={[row]} />);

    const header = screen.getByRole("columnheader", { name: "消息" });
    const cell = screen.getByRole("cell", { name: row.image });
    expect(header).toHaveClass("message-header");
    expect(cell).toHaveClass("message-cell");
    expect(cell).toHaveStyle({ width: "48%", minWidth: "420px", maxWidth: "760px" });
  });

  it("can opt out of pinning when the first column is not an identity field", () => {
    const columns = [
      column.accessor("age", { header: "时间" }),
      column.accessor("status", { header: "状态" }),
    ];

    render(<DataTable columns={columns} data={[row]} stickyFirstColumn={false} />);

    expect(screen.getByRole("columnheader", { name: "时间" })).not.toHaveClass("sticky");
    expect(screen.getByRole("cell", { name: row.age })).not.toHaveClass("sticky");
  });
});

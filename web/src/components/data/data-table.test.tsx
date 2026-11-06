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
    expect(screen.getByRole("columnheader", { name: "名称" })).toHaveClass("sticky", "left-0", "min-w-44");
    expect(screen.getByRole("cell", { name: row.name })).toHaveClass("sticky", "left-0", "bg-card");
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
});

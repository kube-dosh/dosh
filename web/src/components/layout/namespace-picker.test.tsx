import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { NamespaceItem } from "@/api/types";
import { NamespacePicker, type NamespacePlan } from "./namespace-picker";

const { namespaces } = vi.hoisted(() => ({ namespaces: vi.fn() }));

vi.mock("@/api/client", () => ({ api: { namespaces } }));

function namespaceList(items: Array<Pick<NamespaceItem, "name" | "status">>) {
  return {
    items: items.map((item) => ({
      ...item,
      createdAt: "2026-11-02T00:00:00Z",
    })),
    meta: {
      total: items.length,
      totalKnown: true,
      page: 1,
      pageSize: Math.max(items.length, 1),
      totalPages: items.length ? 1 : 0,
      hasMore: false,
    },
  };
}

function renderPicker(props: {
  value: string;
  recommended?: string;
  onPlanChange?: (plan: NamespacePlan) => void;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <NamespacePicker
        id="test-namespace"
        label="目标命名空间"
        value={props.value}
        recommended={props.recommended}
        onChange={() => undefined}
        onPlanChange={props.onPlanChange}
      />
    </QueryClientProvider>,
  );
}

describe("NamespacePicker", () => {
  beforeEach(() => {
    namespaces.mockReset();
  });

  it("offers a missing recommendation for creation, filters Terminating namespaces and searches existing ones", async () => {
    namespaces.mockResolvedValue(
      namespaceList([
        { name: "default", status: "Active" },
        { name: "production", status: "Active" },
        { name: "old", status: "Terminating" },
      ]),
    );
    const onPlanChange = vi.fn();
    renderPicker({
      value: "nfs-provisioning",
      recommended: "nfs-provisioning",
      onPlanChange,
    });

    const trigger = screen.getByRole("combobox", { name: "目标命名空间" });
    await waitFor(() =>
      expect(trigger).toHaveTextContent("nfs-provisioning（推荐，将新建）"),
    );
    await waitFor(() =>
      expect(onPlanChange).toHaveBeenCalledWith(
        expect.objectContaining({
          exists: false,
          recommended: true,
          createRequired: true,
        }),
      ),
    );

    fireEvent.click(trigger);
    expect(
      screen.getByRole("option", { name: "nfs-provisioning（推荐，将新建）" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "old" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByRole("searchbox", { name: "搜索 目标命名空间" }), {
      target: { value: "prod" },
    });
    expect(screen.getByRole("option", { name: "production" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "default" })).not.toBeInTheDocument();
  });

  it("supports manual input and returning to the namespace list", async () => {
    namespaces.mockResolvedValue(
      namespaceList([{ name: "production", status: "Active" }]),
    );
    renderPicker({ value: "storage-team" });

    const trigger = screen.getByRole("combobox", { name: "目标命名空间" });
    await waitFor(() => expect(trigger).not.toBeDisabled());
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole("option", { name: "手动输入" }));

    expect(screen.getByRole("textbox", { name: "目标命名空间" })).toHaveValue(
      "storage-team",
    );
    fireEvent.click(screen.getByRole("button", { name: "返回命名空间列表" }));
    expect(screen.getByRole("combobox", { name: "目标命名空间" })).toBeInTheDocument();
  });

  it("falls back to manual input when namespace listing is forbidden", async () => {
    namespaces.mockRejectedValue(new Error("forbidden"));
    renderPicker({ value: "storage-team" });

    expect(
      await screen.findByRole("textbox", { name: "目标命名空间" }),
    ).toHaveValue("storage-team");
    expect(
      screen.getByText(/无法读取命名空间列表，已切换为手动输入/),
    ).toBeInTheDocument();
  });

  it("warns before installing into Kubernetes system namespaces", async () => {
    namespaces.mockResolvedValue(
      namespaceList([{ name: "kube-system", status: "Active" }]),
    );
    renderPicker({ value: "kube-system" });

    expect(
      await screen.findByText(/属于系统命名空间，安装第三方工作负载可能带来集群级风险/),
    ).toBeInTheDocument();
  });
});

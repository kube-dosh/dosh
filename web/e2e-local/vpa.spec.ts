import { expect, test } from "@playwright/test";

import { expectInsideViewport, expectNoPageOverflow, loginWithToken, token } from "./support";

test("VPA 缺失 CRD 时给出明确提示，配置对话框兼容桌面与移动端", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
    { width: 320, height: 720 },
  ]) {
    await page.setViewportSize(viewport);
    await page.goto("/resilience?namespace=default&tab=vpa");
    await expect(page.getByRole("heading", { name: "弹性与可用性" })).toBeVisible({ timeout: 60_000 });
    await expect(page.getByRole("tab", { name: /VPA 垂直治理/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByText(/集群未安装 autoscaling.k8s.io\/v1 VerticalPodAutoscaler CRD/).first()).toBeVisible({ timeout: 30_000 });
    await expectNoPageOverflow(page, `${viewport.width}px VPA page`);

    await page.getByRole("button", { name: "创建 VPA", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "创建 VPA" });
    await expect(dialog).toBeVisible();
    await expectInsideViewport(page, dialog, `${viewport.width}px VPA dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px VPA dialog`);
    await expect(dialog.getByLabel("名称")).toBeVisible();
    await expect(dialog.getByLabel("更新策略")).toBeVisible();
    await expect(dialog.getByText("容器资源策略", { exact: true })).toBeVisible();

    await dialog.getByRole("combobox", { name: "选择目标工作负载" }).click();
    const targetSearch = page.getByRole("searchbox", { name: "搜索 选择目标工作负载" });
    await expect(targetSearch).toBeVisible();
    await targetSearch.fill("nginx");
    await expect(page.getByRole("option", { name: /nginx/ }).first()).toBeVisible();
    await expectInsideViewport(page, page.getByRole("listbox", { name: "选择目标工作负载选项列表" }), `${viewport.width}px VPA target picker`);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();

    await dialog.getByRole("button", { name: "添加策略" }).click();
    await expect(dialog.getByText("容器资源策略", { exact: true })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px VPA expanded dialog`);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toBeHidden();
  }
});

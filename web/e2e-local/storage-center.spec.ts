import { expect, test, type Locator, type Page } from "@playwright/test";

const clusterName = process.env.DOSH_E2E_CLUSTER_NAME ?? "kubedo-lab";
const token = process.env.DOSH_E2E_TOKEN;

async function loginWithToken(page: Page) {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Kubernetes Token 登录" })).toBeVisible();
  await page.getByLabel("Bearer Token").fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByLabel("当前集群")).toContainText(clusterName);
}

async function expectNoPageOverflow(page: Page, label: string) {
  const widths = await page.evaluate(() => ({
    viewport: window.visualViewport?.width ?? window.innerWidth,
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  expect(Math.max(widths.document, widths.body), `${label} page width`).toBeLessThanOrEqual(widths.viewport + 1);
}

async function expectInsideViewport(page: Page, locator: Locator, label: string) {
  const box = await locator.boundingBox();
  const viewport = page.viewportSize();
  expect(box, `${label} bounding box`).not.toBeNull();
  expect(viewport, `${label} viewport`).not.toBeNull();
  expect(box!.x, `${label} left`).toBeGreaterThanOrEqual(-1);
  expect(box!.y, `${label} top`).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, `${label} right`).toBeLessThanOrEqual(viewport!.width + 1);
  expect(box!.y + box!.height, `${label} bottom`).toBeLessThanOrEqual(viewport!.height + 1);
}

test("生产存储中心默认入口、预检与移动端布局可用", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
    { width: 360, height: 800 },
    { width: 320, height: 720 },
  ]) {
    await page.setViewportSize(viewport);
    await page.goto("/storage");
    await expect(page.getByRole("heading", { name: "存储中心", exact: true })).toBeVisible({ timeout: 60_000 });
    await expect(page.getByRole("tab", { name: "存储中心", exact: true })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tab", { name: /StorageClass/ })).toBeVisible();
    await expect(page.getByRole("tab", { name: /PVC/ })).toBeVisible();
    await expect(page.getByRole("tab", { name: /快照/ })).toBeVisible();
    await expect(page.getByText("Local Path Provisioner", { exact: true }).first()).toBeVisible({ timeout: 60_000 });
    await expect(page.getByRole("heading", { name: "存储方案目录", exact: true })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px storage center`);

    await page.getByRole("button", { name: "查看方案与预检" }).first().click();
    const dialog = page.getByRole("dialog", { name: /Local Path Provisioner/ });
    await expect(dialog).toBeVisible();
    await expectInsideViewport(page, dialog, `${viewport.width}px storage catalog dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px storage catalog dialog`);
    await expect(dialog.getByText("Provisioner（Kubernetes 标识）", { exact: true })).toBeVisible();
    await expect(dialog.getByText("上游项目", { exact: true })).toBeVisible();
    await expect(dialog.getByText("rancher/local-path-provisioner", { exact: true })).toBeVisible();
    await expect(dialog.getByText("预期 StorageClass", { exact: true })).toBeVisible();
    const namespacePicker = dialog.getByRole("combobox", { name: "目标命名空间" });
    await expect(namespacePicker).toBeVisible();
    await namespacePicker.click();
    const namespaceSearch = page.getByRole("searchbox", { name: "搜索 目标命名空间" });
    await expect(namespaceSearch).toBeVisible();
    await namespaceSearch.fill("storage");
    const namespaceOptions = page.getByRole("option");
    await expect(namespaceOptions.first()).toBeVisible();
    const namespaceList = page.getByRole("listbox", { name: "目标命名空间选项列表" });
    await expectInsideViewport(page, namespaceList, `${viewport.width}px namespace picker`);
    await expectNoPageOverflow(page, `${viewport.width}px namespace picker`);
    await page.keyboard.press("Escape");
    await expect(namespaceList).toBeHidden();
    await expect(dialog).toBeVisible();

    if (viewport.width === 1440) {
      await dialog.getByRole("button", { name: "运行预检" }).click();
      await expect(dialog.getByText("预检结果")).toBeVisible({ timeout: 60_000 });
      await expect(dialog.getByText("Chart 来源", { exact: true })).toBeVisible();
      await expectInsideViewport(page, dialog, "desktop storage preflight dialog");
    }

    await page.screenshot({ path: `test-results-local/storage-center-${viewport.width}.png`, fullPage: false });
    await dialog.getByRole("button", { name: "关闭", exact: true }).first().click();
    await expect(dialog).toBeHidden();
  }
});

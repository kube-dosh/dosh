import { expect, test, type Page } from "@playwright/test";

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

test("生产镜像选择器可读取真实仓库与 Tag 且在移动端无溢出", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }, { width: 320, height: 720 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/workloads/deployment/dosh/dosh");
    await expect(page.getByRole("heading", { name: "dosh", exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "更新镜像", exact: true }).click();
    await page.getByRole("button", { name: "浏览镜像仓" }).click();

    const picker = page.getByRole("dialog", { name: "选择容器镜像" });
    await expect(picker).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px registry picker`);
    await expect(picker.getByText(/仓库列表不可用|Tag 列表不可用/)).toHaveCount(0);

    await picker.getByRole("button", { name: "展开仓库列表" }).click();
    await picker.getByLabel("搜索仓库").fill("dosh/dosh");
    const repository = picker.getByRole("option", { name: "dosh/dosh", exact: true });
    await expect(repository).toBeVisible();
    await repository.click();

    await expect(picker.getByRole("listbox", { name: "仓库列表" })).toBeHidden();
    const tagOptions = picker.getByRole("listbox", { name: "Tag 列表" }).getByRole("option");
    await expect(tagOptions.first()).toHaveText("0.1.8");
    const tag = picker.getByRole("option", { name: "0.1.8", exact: true });
    await expect(tag).toBeVisible();
    await tag.click();
    await expect(picker.getByRole("listbox", { name: "Tag 列表" })).toBeHidden();
    await expect(picker.getByText("192.168.122.10:5000/dosh/dosh:0.1.8", { exact: true })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px selected registry image`);

    await page.screenshot({ path: `test-results-local/registry-picker-${viewport.width}.png`, fullPage: false });
    await picker.getByRole("button", { name: "取消", exact: true }).click();
    await page.getByRole("dialog", { name: "更新容器镜像" }).getByRole("button", { name: "取消", exact: true }).click();
  }
});

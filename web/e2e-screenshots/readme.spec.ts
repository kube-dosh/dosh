import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type Page } from "@playwright/test";

const token = process.env.DOSH_E2E_TOKEN;
const outputDir = path.resolve(process.env.DOSH_E2E_SCREENSHOT_DIR ?? "../docs/images/readme");

async function settle(page: Page) {
  await page.waitForLoadState("domcontentloaded");
  await page.waitForTimeout(1_200);
  await page.mouse.move(1, 1);
}

async function capture(page: Page, name: string) {
  await settle(page);
  await page.screenshot({
    path: path.join(outputDir, `${name}.png`),
    animations: "disabled",
    caret: "hide",
    fullPage: false,
  });
}

test("capture README product screenshots", async ({ page }) => {
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  mkdirSync(outputDir, { recursive: true });

  await page.addInitScript(() => {
    localStorage.setItem("dosh-theme", "light");
  });

  await page.goto("/login");
  await page.getByLabel("Bearer Token").fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("资源使用", { exact: true })).toBeVisible();
  await capture(page, "overview");

  await page.getByRole("link", { name: "工作负载", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "工作负载" })).toBeVisible();
  await capture(page, "workloads");

  await page.getByRole("link", { name: "事件", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "集群事件" })).toBeVisible();
  await capture(page, "events");

  await page.getByRole("link", { name: "YAML 创建", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "YAML 创建" })).toBeVisible();
  await capture(page, "yaml-create");
});

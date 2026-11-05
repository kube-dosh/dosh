import { expect, test } from "@playwright/test";

const clusterName = process.env.DOSH_E2E_CLUSTER_NAME ?? "dosh-ui-dynamic-e2e";
const username = process.env.DOSH_E2E_LOCAL_USERNAME ?? "admin";
const password = process.env.DOSH_E2E_LOCAL_PASSWORD;

test("本地认证下展示真实集群上下文并完成只读浏览", async ({ page }) => {
  test.skip(!password, "DOSH_E2E_LOCAL_PASSWORD is required");

  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
  await page.getByLabel("用户名").fill(username);
  await page.getByLabel("密码").fill(password!);
  await page.getByRole("button", { name: "登录", exact: true }).click();

  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByLabel("当前集群")).toContainText(clusterName);
  await expect(page.getByRole("status", { name: "集群连接状态" })).toContainText("集群连接正常");
  await expect(page.getByText(`总览 / ${clusterName}`, { exact: true })).toBeVisible();
  await expect(page.getByText("kubedo-lab", { exact: true })).toHaveCount(0);

  await page.getByRole("link", { name: "节点管理" }).click();
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();
  await expect(page.getByRole("link", { name: "worker-0001", exact: true })).toBeVisible();

  await page.getByRole("link", { name: "审计日志" }).click();
  await expect(page.getByRole("heading", { name: "审计日志" })).toBeVisible();
  await expect(page.getByText("仅展示当前实例最近 1000 条记录", { exact: false })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: username }).first()).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "login" }).first()).toBeVisible();

  await page.getByRole("button", { name: new RegExp(username, "i") }).click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
});

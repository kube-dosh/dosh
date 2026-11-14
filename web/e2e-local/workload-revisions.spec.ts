import { expect, test, type Page } from "@playwright/test";

const clusterName = process.env.DOSH_E2E_CLUSTER_NAME ?? "kubedo-lab";
const targetNamespace = process.env.DOSH_E2E_TARGET_NAMESPACE ?? "dosh";
const token = process.env.DOSH_E2E_TOKEN;

async function loginWithToken(page: Page) {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Kubernetes Token 登录" })).toBeVisible();
  await page.getByLabel("Bearer Token").fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByLabel("当前集群")).toContainText(clusterName);
}

type DeploymentInput = {
  namespace: string;
  name: string;
  replicas: number;
  image: string;
  containerPort: number;
  cpuRequestMilli: number;
  cpuLimitMilli: number;
  memoryRequestBytes: number;
  memoryLimitBytes: number;
};

async function createDeployment(page: Page, input: DeploymentInput) {
  await page.evaluate(async (payload) => {
    const session = await fetch("/api/v1/session").then((response) => response.json()) as { csrfToken: string };
    const response = await fetch("/api/v1/workloads/deployments", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrfToken },
      body: JSON.stringify(payload),
    });
    if (!response.ok) throw new Error(`create deployment failed: ${response.status} ${await response.text()}`);
  }, input);
}

async function deleteDeployment(page: Page, namespace: string, name: string) {
  await page.evaluate(async ({ namespace, name }) => {
    const session = await fetch("/api/v1/session").then((response) => response.json()) as { csrfToken: string };
    const response = await fetch(`/api/v1/workloads/deployment/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}?propagation=Background`, {
      method: "DELETE",
      headers: { "X-CSRF-Token": session.csrfToken },
    });
    if (!response.ok && response.status !== 404) throw new Error(`delete deployment failed: ${response.status} ${await response.text()}`);
  }, { namespace, name });
}

test("Deployment 发布历史可刷新并回滚到上一版本", async ({ page }) => {
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);
  const name = `dosh-e2e-rollback-${Date.now()}`;
  await createDeployment(page, {
    namespace: targetNamespace,
    name,
    replicas: 0,
    image: "registry.k8s.io/pause:3.9",
    containerPort: 80,
    cpuRequestMilli: 10,
    cpuLimitMilli: 50,
    memoryRequestBytes: 16 << 20,
    memoryLimitBytes: 32 << 20,
  });
  try {
    await page.goto(`/workloads/deployment/${targetNamespace}/${name}`);
    await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
    await page.getByRole("button", { name: "更新镜像" }).click();
    const imageDialog = page.getByRole("dialog");
    await imageDialog.getByLabel("镜像地址").fill("registry.k8s.io/pause:3.10");
    await imageDialog.getByRole("button", { name: "确认更新" }).click();
    await expect(page.getByText("镜像已更新，滚动发布已触发")).toBeVisible();

    await page.getByRole("tab", { name: "发布历史" }).click();
    await expect.poll(async () => {
      await page.getByRole("button", { name: "刷新" }).click();
      return page.locator("tbody tr").count();
    }, { timeout: 15_000 }).toBeGreaterThanOrEqual(2);

    const oldRevision = page.locator("tbody tr").filter({ hasText: "pause:3.9" }).first();
    await expect(oldRevision).toBeVisible();
    await oldRevision.getByRole("button", { name: "回滚" }).click();
    const confirm = page.getByRole("alertdialog");
    await expect(confirm).toContainText("回滚到 revision");
    await confirm.getByRole("button", { name: "确认回滚" }).click();
    await expect(page.getByText(/已提交回滚到 revision \d+/)).toBeVisible();

    await expect.poll(async () => {
      await page.getByRole("button", { name: "刷新" }).click();
      const current = page.locator("tbody tr").filter({ hasText: "当前" }).first();
      return await current.textContent() ?? "";
    }, { timeout: 20_000 }).toContain("pause:3.9");
  } finally {
    await deleteDeployment(page, targetNamespace, name);
  }
});

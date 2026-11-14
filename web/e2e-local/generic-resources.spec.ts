import { expect, test, type Locator, type Page } from "@playwright/test";

const clusterName = process.env.DOSH_E2E_CLUSTER_NAME ?? "kubedo-lab";
const targetNamespace = process.env.DOSH_E2E_TARGET_NAMESPACE ?? "dosh";
const token = process.env.DOSH_E2E_TOKEN;

async function loginWithToken(page: import("@playwright/test").Page) {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Kubernetes Token 登录" })).toBeVisible();
  await page.getByLabel("Bearer Token").fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByLabel("当前集群")).toContainText(clusterName);
}

async function replaceEditorText(page: Page, editor: Locator, value: string) {
  await editor.click();
  await expect(editor).toBeFocused();
  await editor.press("ControlOrMeta+A");
  await editor.press("Backspace");
  await page.keyboard.insertText(value);
}

test("通用资源浏览器可发现并浏览 Pod 与 Service", async ({ page }) => {
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  await page.getByRole("link", { name: "资源浏览器" }).click();
  await expect(page.getByRole("heading", { name: "资源浏览器" })).toBeVisible();
  await expect(page.getByText(/共 \d+ 个资源类型/)).toBeVisible();

  await page.getByPlaceholder("搜索 Kind、资源名、API Group 或短名称").fill("Pod");
  await page.getByRole("link", { name: "Pod", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Pod", exact: true })).toBeVisible();
  await expect(page.getByText("Namespaced · core/v1 · 当前读取受 Kubernetes RBAC 约束。")).toBeVisible();

  const firstPod = page.locator("tbody tr").first();
  await expect(firstPod).toBeVisible();
  await firstPod.getByRole("link").first().click();
  await expect(page.getByRole("tab", { name: "概览" })).toBeVisible();
  await page.getByRole("tab", { name: "YAML" }).click();
  await expect(page.locator(".cm-content")).toContainText("apiVersion: v1");
  await expect(page.locator(".cm-content")).toContainText("kind: Pod");
  await expect(page.locator(".cm-content")).not.toContainText("managedFields");

  await page.goto("/resources/_core/v1/services");
  await expect(page.getByRole("heading", { name: "Service", exact: true })).toBeVisible();
  await expect(page.locator("tbody tr").filter({ hasText: "dosh" }).first()).toBeVisible();
});

test("Secret 通用详情遮蔽数据且固定资源页面保留", async ({ page }) => {
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  await page.goto(`/resources/_core/v1/secrets/dosh-auth?namespace=${targetNamespace}`);
  await expect(page.getByRole("heading", { name: "dosh-auth", exact: true })).toBeVisible();
  await expect(page.getByText("数据已遮蔽", { exact: true })).toBeVisible();
  await expect(page.getByText("通用资源视图已遮蔽 Secret 数据；如需查看敏感值，请使用 Secret 专用页面")).toBeVisible();
  await page.getByRole("tab", { name: "YAML" }).click();
  await expect(page.getByText("只读 YAML · 已移除 managedFields；Secret 数据在通用视图中始终遮蔽")).toBeVisible();
  await expect(page.locator(".cm-content")).toContainText("kind: Secret");
  await expect(page.locator(".cm-line").filter({ hasText: "kubectl.kubernetes.io/last-applied-configuration: '[redacted]'" })).toBeVisible();
  const yaml = (await page.locator(".cm-line").allTextContents()).join("\n");
  expect(yaml).not.toMatch(/^\s*(data|stringData):/m);
  expect(yaml).not.toContain("managedFields");

  await page.goto("/pods");
  await expect(page.getByRole("heading", { name: "Pod 实例" })).toBeVisible();
  await page.goto("/network");
  await expect(page.getByRole("heading", { name: "服务与路由" })).toBeVisible();
  await page.goto("/configuration");
  await expect(page.getByRole("heading", { name: "配置中心" })).toBeVisible();
});

test("CRD 可通过通用 YAML 创建、编辑和删除", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);
  const name = `dosh-e2e-cnp-${Date.now()}`;
  const baseYAML = `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: ${name}
  namespace: ${targetNamespace}
spec:
  endpointSelector:
    matchLabels:
      app: dosh-e2e-nonexistent
  egress: []
  ingress: []
`;
  const updatedYAML = baseYAML.replace("app: dosh-e2e-nonexistent", "app: dosh-e2e-changed");

  await page.goto(`/resources/cilium.io/v2/ciliumnetworkpolicies?namespace=${targetNamespace}`);
  await expect(page.getByRole("heading", { name: "CiliumNetworkPolicy", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "创建 YAML" }).click();
  const createDialog = page.getByRole("dialog");
  await expect(createDialog).toContainText("cilium.io/v2");
  const createEditor = createDialog.locator(".cm-content");
  await replaceEditorText(page, createEditor, baseYAML);
  await expect(createEditor).toContainText(`name: ${name}`);
  await expect(createEditor).not.toContainText("name: example");
  const dryRunResponsePromise = page.waitForResponse((response) =>
    response.request().method() === "POST" && response.url().endsWith("/api/v1/resources/cilium.io/v2/ciliumnetworkpolicies"));
  await createDialog.getByRole("button", { name: "Dry Run" }).click();
  const dryRunResponse = await dryRunResponsePromise;
  expect(dryRunResponse.status(), `${await dryRunResponse.text()}\nrequest=${dryRunResponse.request().postData()}`).toBe(201);
  await expect(page.getByText("Dry Run 校验通过，未写入集群")).toBeVisible();
  await createDialog.getByRole("button", { name: "创建", exact: true }).click();
  await expect(page.getByText(`CiliumNetworkPolicy ${name} 已创建`)).toBeVisible();

  await page.getByPlaceholder("搜索名称或命名空间").fill(name);
  const row = page.locator("tbody tr").filter({ hasText: name }).first();
  await expect(row).toBeVisible();
  await row.getByRole("link", { name }).click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();

  await page.getByRole("button", { name: "编辑 YAML" }).click();
  const editDialog = page.getByRole("dialog");
  const editEditor = editDialog.locator(".cm-content");
  await replaceEditorText(page, editEditor, updatedYAML);
  await expect(editEditor).toContainText("app: dosh-e2e-changed");
  await editDialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText(`CiliumNetworkPolicy ${name} 已更新`)).toBeVisible();
  await page.getByRole("tab", { name: "YAML" }).click();
  await expect(page.locator(".cm-content")).toContainText("app: dosh-e2e-changed");

  await page.getByRole("button", { name: "删除", exact: true }).click();
  const deleteDialog = page.getByRole("alertdialog");
  await deleteDialog.getByLabel("输入名称确认").fill(name);
  await deleteDialog.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByText(`CiliumNetworkPolicy ${name} 的删除请求已提交`)).toBeVisible();
  await expect(page).toHaveURL((url) => url.pathname === "/resources/cilium.io/v2/ciliumnetworkpolicies" && url.searchParams.get("namespace") === targetNamespace);
});

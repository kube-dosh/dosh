import { expect, test, type Page } from "@playwright/test";

import {
  clearPortForwardSessions,
  expectInsideViewport,
  expectNoPageOverflow,
  loginWithToken,
  token,
} from "./support";

type TunnelSession = {
  id: string;
  pod: string;
  ports?: Array<{ requestedPort: number; port: number; path: string }>;
  path: string;
};

function declaredPort(page: Page, port: number) {
  return page.locator("button[aria-pressed]").filter({ hasText: new RegExp(`^${port}(?!\\d)`) });
}

async function nginxPodName(page: Page) {
  const response = await page.request.get("/api/v1/pods?namespace=default&search=nginx");
  expect(response.ok(), "list nginx pods").toBeTruthy();
  const body = await response.json() as { items: Array<{ name: string; status: string; ready: string }> };
  const pod = body.items.find((item) => item.status === "Running" && item.ready === "1/1");
  expect(pod, "running nginx pod").toBeTruthy();
  return pod!.name;
}

async function firstTunnel(page: Page) {
  const response = await page.request.get("/api/v1/tunnels");
  expect(response.ok(), "list tunnels").toBeTruthy();
  const body = await response.json() as { items: TunnelSession[] };
  expect(body.items.length, "active tunnel count").toBe(1);
  return body.items[0];
}

test("Pod 多端口隧道支持流式访问、刷新恢复与关闭", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);
  await clearPortForwardSessions(page);
  await page.setViewportSize({ width: 1440, height: 900 });

  const podName = await nginxPodName(page);
  await page.goto(`/pods/default/${podName}?namespace=default&tab=port-forward`);
  await expect(page.getByRole("heading", { name: "新建临时隧道" })).toBeVisible({ timeout: 60_000 });
  await declaredPort(page, 8443).click();
  await expect(declaredPort(page, 8080)).toHaveAttribute("aria-pressed", "true");
  await expect(declaredPort(page, 8443)).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: /建立隧道（2 个端口）/ }).click();
  await expect(page.getByTestId("port-forward-session")).toContainText("请求端口 8080");
  await expect(page.getByTestId("port-forward-session")).toContainText("请求端口 8443");

  const session = await firstTunnel(page);
  expect(session.ports?.map((port) => port.requestedPort).sort((left, right) => left - right)).toEqual([8080, 8443]);
  const httpEndpoint = session.ports!.find((port) => port.requestedPort === 8080)!;
  const tunnelResponse = await page.request.get(httpEndpoint.path);
  expect(tunnelResponse.ok(), "Pod tunnel upstream response").toBeTruthy();
  expect(await tunnelResponse.text()).toContain("Welcome to nginx!");

  await page.reload();
  await expect(page.getByTestId("port-forward-session")).toContainText("请求端口 8080", { timeout: 30_000 });
  await page.getByTestId("port-forward-session").getByRole("button", { name: "关闭", exact: true }).click();
  await expect(page.getByTestId("port-forward-session")).toContainText("尚未创建端口转发");
  await expectNoPageOverflow(page, "desktop Pod port-forward");
});

test("Service 多端口支持指定后端 Pod，桌面与移动端布局可用", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
    { width: 320, height: 720 },
  ]) {
    await clearPortForwardSessions(page);
    await page.setViewportSize(viewport);
    await page.goto("/network/services/default/nginx?namespace=default&tab=port-forward");
    await expect(page.getByRole("heading", { name: "新建临时隧道" })).toBeVisible({ timeout: 60_000 });
    await expectNoPageOverflow(page, `${viewport.width}px service port-forward`);

    const picker = page.getByRole("combobox", { name: "后端 Pod" });
    await expect(picker).toBeVisible();
    await picker.click();
    const search = page.getByRole("searchbox", { name: "搜索 后端 Pod" });
    await expect(search).toBeVisible();
    const podOption = page.getByRole("option", { name: /nginx-.*Ready/ }).first();
    await expect(podOption).toBeVisible();
    await expectInsideViewport(page, page.getByRole("listbox", { name: "后端 Pod选项列表" }), `${viewport.width}px backend Pod list`);
    await search.fill("nginx-");
    await podOption.click();
    const selectedPod = (await picker.textContent())?.match(/nginx-[a-z0-9-]+/)?.[0];
    expect(selectedPod, "selected backend Pod").toBeTruthy();

    await declaredPort(page, 443).click();
    await expect(declaredPort(page, 80)).toHaveAttribute("aria-pressed", "true");
    await expect(declaredPort(page, 443)).toHaveAttribute("aria-pressed", "true");
    await page.getByRole("button", { name: /建立隧道（2 个端口）/ }).click();
    await expect(page.getByTestId("port-forward-session")).toContainText("Service nginx", { timeout: 30_000 });
    await expectNoPageOverflow(page, `${viewport.width}px active service tunnel`);

    const session = await firstTunnel(page);
    expect(session.pod).toBe(selectedPod);
    expect(session.ports?.map((port) => port.requestedPort).sort((left, right) => left - right)).toEqual([80, 443]);
    expect(session.ports?.map((port) => port.port).sort((left, right) => left - right)).toEqual([8080, 8443]);
    const httpEndpoint = session.ports!.find((port) => port.requestedPort === 80)!;
    const tunnelResponse = await page.request.get(httpEndpoint.path);
    expect(tunnelResponse.ok(), `${viewport.width}px service tunnel response`).toBeTruthy();
    expect(await tunnelResponse.text()).toContain("Welcome to nginx!");

    await page.getByTestId("port-forward-session").getByRole("button", { name: "关闭全部" }).click();
    await expect(page.getByTestId("port-forward-session")).toContainText("尚未创建端口转发");
  }
});

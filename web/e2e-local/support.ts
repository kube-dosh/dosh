import { expect, type Locator, type Page } from "@playwright/test";

export const clusterName = process.env.DOSH_E2E_CLUSTER_NAME ?? "kubedo-lab";
export const token = process.env.DOSH_E2E_TOKEN;

export async function loginWithToken(page: Page) {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Kubernetes Token 登录" })).toBeVisible();
  await page.getByLabel("Bearer Token").fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByLabel("当前集群")).toContainText(clusterName);
}

export async function expectNoPageOverflow(page: Page, label: string) {
  const widths = await page.evaluate(() => ({
    viewport: window.visualViewport?.width ?? window.innerWidth,
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  expect(Math.max(widths.document, widths.body), `${label} page width`).toBeLessThanOrEqual(widths.viewport + 1);
}

export async function expectInsideViewport(page: Page, locator: Locator, label: string) {
  const box = await locator.boundingBox();
  const viewport = page.viewportSize();
  expect(box, `${label} bounding box`).not.toBeNull();
  expect(viewport, `${label} viewport`).not.toBeNull();
  expect(box!.x, `${label} left`).toBeGreaterThanOrEqual(-1);
  expect(box!.y, `${label} top`).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, `${label} right`).toBeLessThanOrEqual(viewport!.width + 1);
  expect(box!.y + box!.height, `${label} bottom`).toBeLessThanOrEqual(viewport!.height + 1);
}

export async function clearPortForwardSessions(page: Page) {
  const sessionResponse = await page.request.get("/api/v1/session");
  expect(sessionResponse.ok(), "authenticated session").toBeTruthy();
  const session = await sessionResponse.json() as { csrfToken: string };
  const response = await page.request.delete("/api/v1/tunnels", {
    headers: { "X-CSRF-Token": session.csrfToken },
  });
  expect(response.ok(), "clear port-forward sessions").toBeTruthy();
}

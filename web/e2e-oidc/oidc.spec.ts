import { expect, test, type Page } from "@playwright/test";

const targetNamespace = process.env.DOSH_E2E_TARGET_NAMESPACE ?? "dosh-e2e-target";
const workloadName = "e2e-demo";
const secretName = "e2e-secret";
const idpHeading = "dosh E2E IdP";
const jwtPattern = /eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/;
const testAccessToken = "dosh-e2e-access-token";
const stateTTLMillis = Number(process.env.DOSH_E2E_OIDC_STATE_TTL_MS ?? "5000");
const baseURL = process.env.DOSH_E2E_BASE_URL ?? "https://192.168.122.100:32627";

type SensitiveSurface = {
  responseBodies: string[];
  responseURLs: string[];
  responseRequests: Array<{ method: string; url: string }>;
};

function captureSensitiveSurface(page: Page): SensitiveSurface {
  const captured: SensitiveSurface = { responseBodies: [], responseURLs: [], responseRequests: [] };
  page.on("response", (response) => {
    captured.responseURLs.push(response.url());
    captured.responseRequests.push({ method: response.request().method(), url: response.url() });
    const contentType = response.headers()["content-type"] ?? "";
    if (!/(json|javascript|html|text)/i.test(contentType)) return;
    void response.text().then((body) => {
      if (body.length <= 512 * 1024) captured.responseBodies.push(body);
    }).catch(() => undefined);
  });
  return captured;
}

async function assertNoTokenLeak(page: Page, captured: SensitiveSurface) {
  await page.waitForTimeout(250);
  const browserState = await page.evaluate(() => ({
    localStorage: Object.fromEntries(Object.entries(window.localStorage)),
    sessionStorage: Object.fromEntries(Object.entries(window.sessionStorage)),
    content: document.documentElement.outerHTML,
    url: window.location.href,
  }));
  const cookies = await page.context().cookies();
  const material = JSON.stringify({ browserState, cookies, captured });
  expect(material).not.toMatch(jwtPattern);
  expect(material).not.toContain(testAccessToken);
  expect(browserState.url).not.toMatch(/[?&](code|state|code_verifier)=/);
}

async function loginAs(page: Page, identity: "viewer" | "operator" | "admin") {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  await page.getByRole("button", { name: /使用 .* 登录/ }).click();
  await expect(page.getByRole("heading", { name: idpHeading })).toBeVisible();
  await page.getByRole("button", { name: identity, exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByRole("button", { name: new RegExp(`${identity}@example\\.test`, "i") })).toBeVisible();
  expect(page.url()).not.toMatch(/[?&](code|state)=/);
}

async function startOIDCLogin(page: Page, returnTo = "/") {
  await page.goto(`/api/v1/auth/oidc/login?returnTo=${encodeURIComponent(returnTo)}`);
  await expect(page.getByRole("heading", { name: idpHeading })).toBeVisible();
}

async function oidcStateCookie(page: Page) {
  const cookie = (await page.context().cookies()).find((entry) => entry.name === "dosh_oidc_state");
  expect(cookie, "OIDC state cookie is missing").toBeTruthy();
  return cookie!;
}

async function assertOIDCLoginRejected(page: Page, captured: SensitiveSurface) {
  await expect(page).toHaveURL(/\/login\?error=oidc$/);
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  const sessionStatus = await page.evaluate(async () => {
    const response = await fetch("/api/v1/session", { credentials: "same-origin" });
    return response.status;
  });
  expect(sessionStatus).toBe(401);
  const cookies = await page.context().cookies();
  expect(cookies.find((cookie) => cookie.name === "dosh_session")?.value ?? "").toBe("");
  for (const stateCookie of cookies.filter((cookie) => cookie.name === "dosh_oidc_state")) {
    expect(stateCookie.expires, "stale OIDC state cookie must expire").toBeGreaterThan(0);
    expect(stateCookie.expires * 1000).toBeLessThanOrEqual(Date.now());
  }
  expect(captured.responseRequests.filter((request) => (
    ["POST", "PUT", "PATCH", "DELETE"].includes(request.method) && /\/api\/v1\//.test(request.url)
  ))).toEqual([]);
  await assertNoTokenLeak(page, captured);
}

async function postAPI(page: Page, path: string, body: string): Promise<number> {
  const session = await page.evaluate(async () => {
    const response = await fetch("/api/v1/session", { credentials: "same-origin" });
    if (!response.ok) return null;
    return response.json() as Promise<{ csrfToken: string }>;
  }) as { csrfToken: string } | null;
  expect(session?.csrfToken).toBeTruthy();
  return page.evaluate(async ({ requestPath, requestBody, csrf }) => {
    const response = await fetch(requestPath, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: requestBody,
    });
    return response.status;
  }, { requestPath: path, requestBody: body, csrf: session!.csrfToken });
}

async function deleteAPI(page: Page, path: string): Promise<number> {
  const session = await page.evaluate(async () => {
    const response = await fetch("/api/v1/session", { credentials: "same-origin" });
    if (!response.ok) return null;
    return response.json() as Promise<{ csrfToken: string }>;
  }) as { csrfToken: string } | null;
  if (!session?.csrfToken) return 0;
  return page.evaluate(async ({ requestPath, csrf }) => {
    const response = await fetch(requestPath, {
      method: "DELETE",
      credentials: "same-origin",
      headers: { "X-CSRF-Token": csrf },
    });
    return response.status;
  }, { requestPath: path, csrf: session.csrfToken });
}

test.describe.configure({ mode: "serial" });

test.setTimeout(120_000);

test("viewer 只读、无写按钮、Secret 无权限", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await loginAs(page, "viewer");
  await expect(page.getByRole("link", { name: "审计日志" })).toHaveCount(0);

  await page.goto(`/workloads?namespace=${targetNamespace}`);
  await expect(page.getByRole("heading", { name: "工作负载" })).toBeVisible();
  await page.getByRole("link", { name: workloadName, exact: true }).first().click();
  await expect(page.getByRole("heading", { name: workloadName, exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "扩缩容" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "重启" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "删除" })).toHaveCount(0);
  expect(await postAPI(
    page,
    `/api/v1/workloads/deployment/${targetNamespace}/${workloadName}/scale`,
    JSON.stringify({ replicas: 2 }),
  )).toBe(403);

  await page.goto(`/configuration?namespace=${targetNamespace}`);
  await expect(page.getByRole("heading", { name: "配置与密钥" })).toBeVisible();
  await page.getByRole("tab", { name: /Secret/ }).click();
  await expect(page.getByText(secretName, { exact: true })).toBeVisible();
  await expect(page.getByText("无权限", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "查看明文" })).toHaveCount(0);
  expect(await postAPI(
    page,
    `/api/v1/secrets/${targetNamespace}/${secretName}/reveal`,
    "{}",
  )).toBe(403);

  await assertNoTokenLeak(page, captured);
});

test("筛选条件在真实 Release 刷新、分享和前进后退后保持", async ({ page, context }) => {
  const captured = captureSensitiveSurface(page);
  await loginAs(page, "viewer");

  const filterPath = `/workloads?namespace=${encodeURIComponent(targetNamespace)}&kind=deployment&q=${workloadName}`;
  await page.goto(filterPath);
  const search = page.getByPlaceholder("搜索工作负载名称");
  const namespace = page.getByRole("combobox", { name: "命名空间筛选" });
  const kind = page.getByRole("combobox", { name: "工作负载类型筛选" });
  await expect(search).toHaveValue(workloadName);
  await expect(namespace).toContainText(targetNamespace);
  await expect(kind).toContainText("deployment");
  await expect(page.getByRole("link", { name: workloadName, exact: true })).toBeVisible();

  await page.reload();
  await expect(search).toHaveValue(workloadName);
  await expect(namespace).toContainText(targetNamespace);
  await expect(page.getByRole("link", { name: workloadName, exact: true })).toBeVisible();

  const sharedURL = page.url();
  const sharedPage = await context.newPage();
  await sharedPage.goto(sharedURL);
  await expect(sharedPage.getByPlaceholder("搜索工作负载名称")).toHaveValue(workloadName);
  await expect(sharedPage.getByRole("combobox", { name: "命名空间筛选" })).toContainText(targetNamespace);
  await expect(sharedPage.getByRole("link", { name: workloadName, exact: true })).toBeVisible();
  await sharedPage.close();

  await page.goto("/nodes");
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/kind=deployment/);
  await expect(search).toHaveValue(workloadName);
  await page.goForward();
  await expect(page).toHaveURL(/\/nodes$/);
  await page.goBack();
  await expect(page).toHaveURL(/namespace=/);
  await expect(namespace).toContainText(targetNamespace);

  await assertNoTokenLeak(page, captured);
});

test("operator 仅授权命名空间可写", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await loginAs(page, "operator");
  await page.goto(`/workloads/deployment/${targetNamespace}/${workloadName}`);
  await expect(page.getByRole("heading", { name: workloadName, exact: true })).toBeVisible();

  await page.getByRole("button", { name: "扩缩容" }).click();
  await expect(page.getByRole("dialog", { name: "调整副本数" })).toBeVisible();
  await page.getByLabel("期望副本数").fill("1");
  await page.getByRole("button", { name: "确认调整" }).click();
  await expect(page.getByText("副本数已调整为 1", { exact: true })).toBeVisible();

  expect(await postAPI(
    page,
    `/api/v1/workloads/deployment/default/${workloadName}/scale`,
    JSON.stringify({ replicas: 1 }),
  )).toBe(403);
  await assertNoTokenLeak(page, captured);
});

test("operator 可通过向导创建并验证 Deployment", async ({ page }, testInfo) => {
  const captured = captureSensitiveSurface(page);
  const createdName = `e2e-ui-${testInfo.project.name.toLowerCase()}`;
  const detailPath = `/workloads/deployment/${targetNamespace}/${createdName}`;
  await loginAs(page, "operator");
  await deleteAPI(page, `/api/v1/workloads/deployment/${targetNamespace}/${createdName}`);
  try {
    await page.goto(`/workloads?namespace=${targetNamespace}`);
    await page.getByRole("button", { name: "创建 Deployment" }).click();
    const dialog = page.getByRole("dialog", { name: "创建 Deployment" });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("Deployment 名称").fill(createdName);
    await dialog.getByRole("button", { name: "下一步" }).click();
    await expect(dialog.getByLabel("容器镜像")).toHaveValue("nginx:alpine");
    await dialog.getByRole("button", { name: "下一步" }).click();
    await expect(dialog.getByText("YAML 预览", { exact: true })).toBeVisible();
    await expect(dialog.locator("pre")).toContainText("kind: Deployment");
    await expect(dialog.locator("pre")).toContainText(`name: ${createdName}`);
    await dialog.getByRole("button", { name: "创建 Deployment" }).click();
    await expect(page.getByRole("heading", { name: createdName, exact: true })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${detailPath}$`));
    await expect(page.getByText("nginx:alpine", { exact: true }).first()).toBeVisible();
    await page.goto(`/workloads?namespace=${targetNamespace}&q=${createdName}`);
    await expect(page.getByRole("link", { name: createdName, exact: true })).toBeVisible();
  } finally {
    const deleteStatus = await deleteAPI(page, `/api/v1/workloads/deployment/${targetNamespace}/${createdName}`);
    expect([202, 404]).toContain(deleteStatus);
  }
  await assertNoTokenLeak(page, captured);
});

test("admin 可读节点，RP logout 清除会话", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await loginAs(page, "admin");
  await page.goto("/nodes");
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();
  await expect(page.getByRole("link", { name: "worker-0001", exact: true })).toBeVisible();

  await page.getByRole("button", { name: /admin@example\.test/i }).click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  const sessionStatus = await page.evaluate(async () => {
    const response = await fetch("/api/v1/session", { credentials: "same-origin" });
    return response.status;
  });
  expect(sessionStatus).toBe(401);
  await assertNoTokenLeak(page, captured);
});

test("admin 可查询真实审计历史并按 URL 过滤", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await loginAs(page, "admin");

  await page.getByRole("link", { name: "审计日志" }).click();
  await expect(page.getByRole("heading", { name: "审计日志" })).toBeVisible();
  await expect(page.getByText("仅展示当前实例最近 1000 条记录", { exact: false })).toBeVisible();

  await page.goto(`/audit?namespace=${encodeURIComponent(targetNamespace)}&action=scale&result=success&q=operator`);
  await expect(page.getByRole("cell", { name: "operator@example.test" }).first()).toBeVisible();
  await expect(page.getByRole("cell", { name: new RegExp(workloadName) }).first()).toBeVisible();

  const auditRow = page.getByRole("row").filter({ hasText: "operator@example.test" }).first();
  await auditRow.click();
  const dialog = page.getByRole("dialog", { name: /审计记录 #[0-9]+/ });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("operator@example.test");
  await expect(dialog).toContainText("scale");
  await expect(dialog).toContainText(targetNamespace);
  await expect(dialog).toContainText("请求 ID");

  await assertNoTokenLeak(page, captured);
});

test("OIDC 拒绝缺失 state Cookie 的回调", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await startOIDCLogin(page, "/nodes");
  await page.context().clearCookies();
  await page.getByRole("button", { name: "viewer", exact: true }).click();
  await assertOIDCLoginRejected(page, captured);
});

test("OIDC 拒绝被篡改的 state Cookie", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await startOIDCLogin(page, "/nodes");
  const stateCookie = await oidcStateCookie(page);
  await page.context().clearCookies();
  await page.context().addCookies([{ ...stateCookie, value: `${stateCookie.value.slice(0, -4)}AAAA` }]);
  await page.getByRole("button", { name: "viewer", exact: true }).click();
  await assertOIDCLoginRejected(page, captured);
});

test("OIDC 拒绝与 Cookie 不匹配的 state 参数", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await startOIDCLogin(page, "/nodes");
  await oidcStateCookie(page);
  await page.goto(`${baseURL}/api/v1/auth/oidc/callback?code=invalid&state=wrong-state`);
  await assertOIDCLoginRejected(page, captured);
});

test("OIDC 拒绝过期 state", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  expect(stateTTLMillis).toBeGreaterThan(0);
  await startOIDCLogin(page, "/nodes");
  const startedAt = Date.now();
  await page.waitForTimeout(stateTTLMillis + 1500);
  await page.getByRole("button", { name: "viewer", exact: true }).click();
  expect(Date.now() - startedAt).toBeGreaterThan(stateTTLMillis);
  await assertOIDCLoginRejected(page, captured);
});

test("OIDC 拒绝 nonce mismatch", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await startOIDCLogin(page, "/nodes");
  const authorizationURL = new URL(page.url());
  authorizationURL.searchParams.set("fault", "nonce_mismatch");
  await page.goto(authorizationURL.toString());
  await expect(page.getByRole("heading", { name: idpHeading })).toBeVisible();
  await page.getByRole("button", { name: "viewer", exact: true }).click();
  await assertOIDCLoginRejected(page, captured);
});

test("OIDC 拒绝重放 authorization code", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  await startOIDCLogin(page, "/nodes");
  const stateCookie = await oidcStateCookie(page);
  const callbackRequest = page.waitForRequest((request) => {
    const requestURL = new URL(request.url());
    return requestURL.pathname === "/api/v1/auth/oidc/callback" && requestURL.searchParams.has("code");
  });
  await page.getByRole("button", { name: "viewer", exact: true }).click();
  const callbackURL = (await callbackRequest).url();
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();

  await page.context().clearCookies();
  await page.context().addCookies([stateCookie]);
  await page.goto(callbackURL);
  await assertOIDCLoginRejected(page, captured);
});

test("OIDC 拒绝开放重定向 returnTo", async ({ page }) => {
  const captured = captureSensitiveSurface(page);
  const expectedOrigin = new URL(baseURL).origin;
  for (const returnTo of ["//evil.example.test/steal", "/\\evil.example.test/steal"]) {
    await page.context().clearCookies();
    await startOIDCLogin(page, returnTo);
    await page.getByRole("button", { name: "admin", exact: true }).click();
    await page.waitForURL((url) => url.origin === expectedOrigin && url.pathname === "/", { timeout: 30_000 });
    await expect(page.getByRole("button", { name: /admin@example\.test/i })).toBeVisible({ timeout: 30_000 });
    expect(new URL(page.url()).origin).toBe(expectedOrigin);
    expect(page.url()).not.toContain("evil.example.test");
  }
  await assertNoTokenLeak(page, captured);
});

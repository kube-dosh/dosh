import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const targetNamespace = process.env.DOSH_E2E_TARGET_NAMESPACE ?? "dosh-e2e-target";
const idpHeading = "dosh E2E IdP";
const workloadName = "e2e-demo";

type FocusDescriptor = {
  tag: string;
  role: string;
  text: string;
  ariaLabel: string;
  id: string;
  placeholder: string;
};

function isButton(focus: FocusDescriptor) {
  return focus.tag === "BUTTON" || focus.role === "button";
}

async function describeActiveElement(page: Page): Promise<FocusDescriptor> {
  return page.evaluate(() => {
    const active = document.activeElement as HTMLElement | null;
    return {
      tag: active?.tagName ?? "",
      role: active?.getAttribute("role") ?? "",
      text: (active?.innerText ?? "").replace(/\s+/g, " ").trim(),
      ariaLabel: active?.getAttribute("aria-label") ?? "",
      id: active?.id ?? "",
      placeholder: active?.getAttribute("placeholder") ?? "",
    };
  });
}

async function pressTabUntil(
  page: Page,
  description: string,
  predicate: (focus: FocusDescriptor) => boolean,
  maxTabs = 300,
) {
  for (let index = 0; index <= maxTabs; index += 1) {
    const focus = await describeActiveElement(page);
    if (predicate(focus)) return focus;
    await page.keyboard.press("Tab");
  }
  throw new Error(`Unable to focus ${description} with the keyboard after ${maxTabs} tab presses`);
}

async function expectVisibleFocusRing(page: Page) {
  const styles = await page.evaluate(() => {
    const active = document.activeElement as HTMLElement | null;
    if (!active) return null;
    const style = getComputedStyle(active);
    return {
      boxShadow: style.boxShadow,
      outlineStyle: style.outlineStyle,
      outlineWidth: style.outlineWidth,
    };
  });
  expect(styles, "keyboard focus must land on an interactive element").not.toBeNull();
  expect(
    styles!.boxShadow !== "none" || (styles!.outlineStyle !== "none" && parseFloat(styles!.outlineWidth) > 0),
    "keyboard focus must have a visible focus ring",
  ).toBe(true);
}

async function loginAs(page: Page, identity: "viewer" | "operator" | "admin") {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  await page.getByRole("button", { name: /使用 .* 登录/ }).click();
  await expect(page.getByRole("heading", { name: idpHeading })).toBeVisible();
  await page.getByRole("button", { name: identity, exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
}

async function keyboardLogin(page: Page, identity: "viewer" | "operator" | "admin") {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  await pressTabUntil(
    page,
    "OIDC login button",
    (focus) => isButton(focus) && focus.text.includes("使用 ") && focus.text.endsWith(" 登录"),
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: idpHeading })).toBeVisible();
  await pressTabUntil(
    page,
    `${identity} identity button`,
    (focus) => isButton(focus) && focus.text === identity,
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
}

async function setTheme(page: Page, theme: "light" | "dark") {
  await page.evaluate((value) => localStorage.setItem("dosh-theme", value), theme);
  await page.reload();
  if (theme === "dark") await expect(page.locator("html")).toHaveClass(/dark/);
  else await expect(page.locator("html")).not.toHaveClass(/dark/);
}

async function waitForVisualStability(page: Page) {
  await page.evaluate(async () => {
    await document.fonts?.ready;
    const finiteAnimations = document.getAnimations().filter((animation) => {
      const endTime = animation.effect?.getComputedTiming().endTime;
      return typeof endTime === "number" && Number.isFinite(endTime);
    });
    await Promise.all(finiteAnimations.map((animation) => animation.finished.catch(() => undefined)));
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
  });
}

async function expectNoSeriousAccessibilityViolations(page: Page, context: string) {
  await waitForVisualStability(page);
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  const blocking = results.violations.filter((violation) => (
    violation.impact === "serious" || violation.impact === "critical"
  ));
  expect(blocking, `${context} has accessibility violations:\n${JSON.stringify(blocking, null, 2)}`).toEqual([]);
}

async function logout(page: Page) {
  const response = await page.request.get("/api/v1/auth/oidc/logout", { maxRedirects: 0 });
  expect(response.status()).toBe(302);
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  await page.evaluate(() => localStorage.clear());
}

test("真实 Release 暗色主题切换、持久化且无亮色残留", async ({ page }) => {
  test.setTimeout(60_000);
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/login");
  await page.evaluate(() => localStorage.clear());
  await loginAs(page, "admin");

  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await page.getByRole("button", { name: "切换为深色主题" }).click();

  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("button", { name: "切换为浅色主题" })).toHaveAttribute("aria-pressed", "true");
  await expect.poll(() => page.evaluate(() => localStorage.getItem("dosh-theme"))).toBe("dark");
  await expect.poll(() => page.evaluate(() => document.documentElement.style.colorScheme)).toBe("dark");
  await expect.poll(() => page.evaluate(() => (
    getComputedStyle(document.documentElement).getPropertyValue("--background").trim()
  ))).toBe("222 28% 9%");

  await page.reload();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(page.getByRole("button", { name: "切换为浅色主题" })).toBeVisible();

  await page.getByRole("button", { name: "切换为浅色主题" }).click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await expect.poll(() => page.evaluate(() => localStorage.getItem("dosh-theme"))).toBe("light");
  await expect.poll(() => page.evaluate(() => (
    getComputedStyle(document.documentElement).getPropertyValue("--background").trim()
  ))).toBe("216 33% 97%");

  await logout(page);
});

test("真实 Release 登录页和核心运维页在亮暗主题下无严重可访问性问题", async ({ page }) => {
  test.setTimeout(180_000);
  await page.goto("/login");
  await page.evaluate(() => localStorage.clear());

  for (const theme of ["light", "dark"] as const) {
    await page.goto("/login");
    await setTheme(page, theme);
    await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
    await expectNoSeriousAccessibilityViolations(page, `login/${theme}`);
  }

  await loginAs(page, "admin");
  const podName = await page.evaluate(async () => {
    const response = await fetch("/api/v1/pods?namespace=kube-system", { credentials: "same-origin" });
    if (!response.ok) return "";
    const body = await response.json() as { items?: Array<{ name?: string }> };
    return body.items?.[0]?.name ?? "";
  });
  expect(podName, "real cluster should expose at least one kube-system pod").not.toBe("");

  const pages = [
    { path: "/", heading: "集群总览" },
    { path: "/nodes", heading: "节点管理" },
    { path: `/workloads/deployment/${targetNamespace}/${workloadName}`, heading: workloadName },
    { path: `/pods/kube-system/${encodeURIComponent(podName)}`, heading: podName },
  ];

  for (const theme of ["light", "dark"] as const) {
    await setTheme(page, theme);
    for (const target of pages) {
      await page.goto(target.path);
      await expect(page.getByRole("heading", { name: target.heading, exact: true })).toBeVisible();
      await expectNoSeriousAccessibilityViolations(page, `${target.path}/${theme}`);
    }
  }

  await logout(page);
});

test("真实 Release 仅用键盘完成登录、导航、扩缩容确认和退出", async ({ page }) => {
  test.setTimeout(120_000);
  await keyboardLogin(page, "operator");

  await pressTabUntil(page, "node navigation link", (focus) => focus.tag === "A" && focus.text === "节点管理");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();

  await pressTabUntil(page, "workload navigation link", (focus) => focus.tag === "A" && focus.text === "工作负载");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "工作负载" })).toBeVisible();

  await pressTabUntil(
    page,
    "workload search input",
    (focus) => focus.tag === "INPUT" && focus.placeholder === "搜索工作负载名称",
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.type(workloadName);

  await pressTabUntil(
    page,
    `${workloadName} workload link`,
    (focus) => focus.tag === "A" && focus.text === workloadName,
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: workloadName, exact: true })).toBeVisible();

  await pressTabUntil(page, "scale button", (focus) => isButton(focus) && focus.text === "扩缩容");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "调整副本数" })).toBeVisible();

  await pressTabUntil(page, "replica input", (focus) => focus.tag === "INPUT" && focus.id === "replicas");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Control+A");
  await page.keyboard.type("1");

  await pressTabUntil(page, "scale confirm button", (focus) => isButton(focus) && focus.text === "确认调整");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByText("副本数已调整为 1", { exact: true })).toBeVisible();

  await pressTabUntil(
    page,
    "operator user menu",
    (focus) => isButton(focus) && focus.text.includes("operator@example.test"),
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");

  await pressTabUntil(page, "logout menu item", (focus) => focus.role === "menuitem" && focus.text === "退出登录");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "企业身份登录" })).toBeVisible();
  const sessionStatus = await page.evaluate(async () => {
    const response = await fetch("/api/v1/session", { credentials: "same-origin" });
    return response.status;
  });
  expect(sessionStatus).toBe(401);
  await page.evaluate(() => localStorage.clear());
});

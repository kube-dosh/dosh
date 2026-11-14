import { expect, test } from "@playwright/test";

const clusterName = process.env.DOSH_E2E_CLUSTER_NAME ?? "kubedo-lab";
const token = process.env.DOSH_E2E_TOKEN;

async function loginWithToken(page: import("@playwright/test").Page) {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Kubernetes Token 登录" })).toBeVisible();
  await page.getByLabel("Bearer Token").fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByLabel("当前集群")).toContainText(clusterName);
}

test("治理页在移动端不产生横向裁切", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/governance");
    await expect(page.getByRole("heading", { name: "治理与策略", exact: true })).toBeVisible();
    await expect(page.locator('section[aria-labelledby^="governance-"] a[href^="/resources/"]').first()).toBeVisible();

    const layout = await page.evaluate(() => {
      const main = document.querySelector("main")!;
      const mainRect = main.getBoundingClientRect();
      const mainStyle = getComputedStyle(main);
      const contentLeft = mainRect.left + Number.parseFloat(mainStyle.paddingLeft);
      const contentRight = mainRect.right - Number.parseFloat(mainStyle.paddingRight);
      const viewportWidth = window.visualViewport?.width ?? window.innerWidth;
      const cards = Array.from(document.querySelectorAll<HTMLAnchorElement>('section[aria-labelledby^="governance-"] a[href^="/resources/"]'));
      const outside = cards.filter((card) => {
        const rect = card.getBoundingClientRect();
        return rect.left < contentLeft - 1 || rect.right > contentRight + 1;
      }).map((card) => ({ text: card.textContent?.trim().slice(0, 80), left: card.getBoundingClientRect().left, right: card.getBoundingClientRect().right, contentLeft, contentRight }));
      const clipped = cards.flatMap((card) => {
        const cardRect = card.getBoundingClientRect();
        return Array.from(card.querySelectorAll<HTMLElement>("*")).filter((child) => {
          const rect = child.getBoundingClientRect();
          return rect.width > 0 && (rect.left < cardRect.left - 1 || rect.right > cardRect.right + 1);
        }).map((child) => ({ card: card.textContent?.trim().slice(0, 80), tag: child.tagName, className: child.className, text: child.textContent?.trim().slice(0, 80) }));
      });
      return {
        viewportWidth,
        documentWidth: document.documentElement.scrollWidth,
        bodyWidth: document.body.scrollWidth,
        cardCount: cards.length,
        outside,
        clipped,
      };
    });

    expect(layout.cardCount, `${viewport.width}px governance cards`).toBeGreaterThan(0);
    expect(layout.outside, `${viewport.width}px governance cards outside main content`).toEqual([]);
    expect(layout.clipped, `${viewport.width}px governance card descendants clipped`).toEqual([]);
    expect(Math.max(layout.documentWidth, layout.bodyWidth), `${viewport.width}px page width`).toBeLessThanOrEqual(layout.viewportWidth + 1);
  }
});

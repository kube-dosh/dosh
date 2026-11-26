import { expect, test } from "@playwright/test";

import { expectInsideViewport, expectNoPageOverflow, loginWithToken, token } from "./support";

test("OCI Chart 供应链策略与仓库认证可编辑，桌面与移动端无溢出", async ({ page }) => {
  test.slow();
  test.skip(!token, "DOSH_E2E_TOKEN is required");
  await loginWithToken(page);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
    { width: 320, height: 720 },
  ]) {
    await page.setViewportSize(viewport);
    await page.goto("/helm/repositories");
    await expect(page.getByRole("heading", { name: "Chart 仓库", exact: true })).toBeVisible({ timeout: 60_000 });
    await expectNoPageOverflow(page, `${viewport.width}px Helm repositories`);

    const repositoryCard = page.getByRole("heading", { name: "local-path-provisioner", exact: true })
      .locator("xpath=ancestor::div[contains(concat(' ', normalize-space(@class), ' '), ' shadow-panel ')][1]");
    await expect(repositoryCard).toBeVisible({ timeout: 60_000 });
    await expect(repositoryCard).toContainText("oci://dp.apps.rancher.io/charts/local-path-provisioner");
    await repositoryCard.getByRole("button", { name: "编辑", exact: true }).click();

    const dialog = page.getByRole("dialog", { name: "编辑 Chart 仓库" });
    await expect(dialog).toBeVisible();
    await expectInsideViewport(page, dialog, `${viewport.width}px Helm repository dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px Helm repository dialog`);
    await expect(dialog.getByTestId("helm-supply-chain-policy")).toBeVisible();
    await expect(dialog.getByText("OCI 供应链验证", { exact: true })).toBeVisible();

    const authSelect = dialog.getByRole("combobox", { name: "认证方式" });
    await expect(authSelect).toBeVisible();
    await authSelect.click();
    await page.getByRole("option", { name: "Bearer Token" }).click();
    await expect(dialog.getByLabel("Bearer Token")).toBeVisible();

    const policySelect = dialog.getByRole("combobox", { name: "OCI 供应链验证模式" });
    await policySelect.click();
    await page.getByRole("option", { name: "Cosign 公钥" }).click();
    await expect(dialog.getByRole("button", { name: "添加公钥" })).toBeVisible();
    await dialog.getByRole("button", { name: "添加公钥" }).click();
    await expect(dialog.getByLabel("公钥 1")).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px Cosign key policy`);

    await policySelect.click();
    await page.getByRole("option", { name: "Sigstore Keyless" }).click();
    await expect(dialog.getByRole("button", { name: "添加 Keyless 身份" })).toBeVisible();
    await dialog.getByRole("button", { name: "添加 Keyless 身份" }).click();
    await expect(dialog.getByLabel("Issuer").last()).toBeVisible();
    await expect(dialog.getByLabel("Subject").last()).toBeVisible();
    await expectInsideViewport(page, dialog, `${viewport.width}px Keyless policy dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px Keyless policy`);

    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toBeHidden();
  }
});

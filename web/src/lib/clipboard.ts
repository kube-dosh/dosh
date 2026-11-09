export async function copyText(value: string): Promise<void> {
  const clipboard = globalThis.navigator?.clipboard;
  if (clipboard && globalThis.isSecureContext) {
    try {
      await clipboard.writeText(value);
      return;
    } catch {
      // Fall through to the legacy path for browsers that deny the async API.
    }
  }

  if (typeof document === "undefined" || typeof document.execCommand !== "function") {
    throw new Error("clipboard is unavailable");
  }

  const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  try {
    textarea.focus();
    textarea.select();
    if (!document.execCommand("copy")) throw new Error("clipboard copy was rejected");
  } finally {
    textarea.remove();
    previousFocus?.focus({ preventScroll: true });
  }
}

export type SecretValueKind = "text" | "binary" | "invalid";

export interface DecodedSecretValue {
  kind: SecretValueKind;
  display: string;
  copyValue: string;
}

const nonTextControlCharacters = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/;

function decodeBase64(value: string): Uint8Array {
  const normalized = value
    .replace(/\s+/g, "")
    .replace(/-/g, "+")
    .replace(/_/g, "/")
    .replace(/=+$/, "");
  if (!/^[A-Za-z0-9+/]*$/.test(normalized) || normalized.length % 4 === 1) {
    throw new Error("invalid base64");
  }
  const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), "=");
  const decoded = atob(padded);
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

export function decodeSecretValue(encoded: string): DecodedSecretValue {
  let bytes: Uint8Array;
  try {
    bytes = decodeBase64(encoded);
  } catch {
    return { kind: "invalid", display: encoded, copyValue: encoded };
  }
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    if (nonTextControlCharacters.test(text)) {
      return { kind: "binary", display: encoded, copyValue: encoded };
    }
    return { kind: "text", display: text, copyValue: text };
  } catch {
    return { kind: "binary", display: encoded, copyValue: encoded };
  }
}

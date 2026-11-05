const ESC = 0x1b;
const BEL = 0x07;
const BACKSLASH = 0x5c;
const LINE_FEED = 0x0a;
const TAB = 0x09;

type ParserState = "text" | "escape" | "escape-intermediate" | "csi" | "osc" | "osc-escape" | "string" | "string-escape";

export interface IncrementalLogSanitizer {
  push(chunk: string): string;
  flush(): string;
}

function isRemovedControl(codePoint: number): boolean {
  if (codePoint === TAB || codePoint === LINE_FEED) return false;
  if (codePoint < 0x20 || (codePoint >= 0x7f && codePoint <= 0x9f)) return true;
  return (codePoint >= 0x202a && codePoint <= 0x202e)
    || (codePoint >= 0x2066 && codePoint <= 0x2069);
}

export function createLogSanitizer(): IncrementalLogSanitizer {
  let state: ParserState = "text";

  return {
    push(chunk: string): string {
      let output = "";

      for (const character of chunk) {
        const codePoint = character.codePointAt(0) ?? 0;

        switch (state) {
          case "text":
            if (codePoint === ESC) {
              state = "escape";
            } else if (!isRemovedControl(codePoint)) {
              output += character;
            }
            break;
          case "escape":
            if (codePoint === 0x5b) {
              state = "csi";
            } else if (codePoint === 0x5d) {
              state = "osc";
            } else if (codePoint === 0x50 || codePoint === 0x58 || codePoint === 0x5e || codePoint === 0x5f) {
              state = "string";
            } else if (codePoint >= 0x20 && codePoint <= 0x2f) {
              state = "escape-intermediate";
            } else {
              state = "text";
              if (!isRemovedControl(codePoint)) output += character;
            }
            break;
          case "escape-intermediate":
            if (codePoint >= 0x30 && codePoint <= 0x7e) {
              state = "text";
            } else if (!(codePoint >= 0x20 && codePoint <= 0x2f)) {
              state = "text";
              if (!isRemovedControl(codePoint)) output += character;
            }
            break;
          case "csi":
            if (codePoint >= 0x40 && codePoint <= 0x7e) {
              state = "text";
            } else if (codePoint === ESC) {
              state = "escape";
            } else if (codePoint === LINE_FEED) {
              state = "text";
              output += "\n";
            }
            break;
          case "osc":
            if (codePoint === BEL) {
              state = "text";
            } else if (codePoint === ESC) {
              state = "osc-escape";
            } else if (codePoint === LINE_FEED) {
              state = "text";
              output += "\n";
            }
            break;
          case "osc-escape":
            if (codePoint === BACKSLASH) {
              state = "text";
            } else if (codePoint === ESC) {
              // Keep waiting for the string terminator.
            } else {
              state = "osc";
            }
            break;
          case "string":
            if (codePoint === ESC) {
              state = "string-escape";
            } else if (codePoint === LINE_FEED) {
              state = "text";
              output += "\n";
            }
            break;
          case "string-escape":
            if (codePoint === BACKSLASH) {
              state = "text";
            } else if (codePoint === ESC) {
              // Keep waiting for the string terminator.
            } else {
              state = "string";
            }
            break;
        }
      }

      return output;
    },
    flush(): string {
      state = "text";
      return "";
    },
  };
}

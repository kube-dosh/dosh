export type DiffKind = "context" | "added" | "removed" | "meta";

export interface DiffLine {
  kind: DiffKind;
  text: string;
  oldLine?: number;
  newLine?: number;
}

const MAX_LCS_LINES = 600;

export function diffYAML(before: string, after: string): DiffLine[] {
  const oldLines = before.replace(/\r\n/g, "\n").split("\n");
  const newLines = after.replace(/\r\n/g, "\n").split("\n");
  let prefix = 0;
  while (prefix < oldLines.length && prefix < newLines.length && oldLines[prefix] === newLines[prefix]) prefix++;
  let oldEnd = oldLines.length;
  let newEnd = newLines.length;
  while (oldEnd > prefix && newEnd > prefix && oldLines[oldEnd - 1] === newLines[newEnd - 1]) {
    oldEnd--;
    newEnd--;
  }

  const result: DiffLine[] = oldLines.slice(0, prefix).map((text, index) => ({ kind: "context", text, oldLine: index + 1, newLine: index + 1 }));
  const oldMiddle = oldLines.slice(prefix, oldEnd);
  const newMiddle = newLines.slice(prefix, newEnd);
  if (oldMiddle.length > MAX_LCS_LINES || newMiddle.length > MAX_LCS_LINES) {
    result.push({ kind: "meta", text: `变更过大，仅显示移除 ${oldMiddle.length} 行、新增 ${newMiddle.length} 行；请以 YAML 全文为准` });
    oldMiddle.forEach((text, index) => result.push({ kind: "removed", text, oldLine: prefix + index + 1 }));
    newMiddle.forEach((text, index) => result.push({ kind: "added", text, newLine: prefix + index + 1 }));
  } else {
    result.push(...middleDiff(oldMiddle, newMiddle, prefix));
  }
  const suffixOldStart = oldEnd;
  const suffixNewStart = newEnd;
  const suffix = oldLines.slice(oldEnd);
  suffix.forEach((text, index) => result.push({ kind: "context", text, oldLine: suffixOldStart + index + 1, newLine: suffixNewStart + index + 1 }));
  return result;
}

function middleDiff(oldLines: string[], newLines: string[], offset: number): DiffLine[] {
  const rows = oldLines.length + 1;
  const cols = newLines.length + 1;
  const table = Array.from({ length: rows }, () => new Uint16Array(cols));
  for (let i = oldLines.length - 1; i >= 0; i--) {
    for (let j = newLines.length - 1; j >= 0; j--) {
      table[i][j] = oldLines[i] === newLines[j]
        ? table[i + 1][j + 1] + 1
        : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < oldLines.length || j < newLines.length) {
    if (i < oldLines.length && j < newLines.length && oldLines[i] === newLines[j]) {
      out.push({ kind: "context", text: oldLines[i], oldLine: offset + i + 1, newLine: offset + j + 1 });
      i++;
      j++;
    } else if (j < newLines.length && (i >= oldLines.length || table[i][j + 1] >= table[i + 1][j])) {
      out.push({ kind: "added", text: newLines[j], newLine: offset + j + 1 });
      j++;
    } else {
      out.push({ kind: "removed", text: oldLines[i], oldLine: offset + i + 1 });
      i++;
    }
  }
  return out;
}

export function diffStats(lines: DiffLine[]): { added: number; removed: number } {
  return lines.reduce((stats, line) => {
    if (line.kind === "added") stats.added++;
    if (line.kind === "removed") stats.removed++;
    return stats;
  }, { added: 0, removed: 0 });
}

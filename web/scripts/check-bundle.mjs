import { readFileSync } from "node:fs";
import { basename, join } from "node:path";
import { gzipSync } from "node:zlib";

const limit = 500 * 1024;
const dist = join(process.cwd(), "dist");
const html = readFileSync(join(dist, "index.html"), "utf8");
const assets = [...html.matchAll(/(?:src|href)="(\/assets\/[^"]+\.js)"/g)].map((match) => match[1]);
const unique = [...new Set(assets)];
let total = 0;

for (const asset of unique) {
  const bytes = gzipSync(readFileSync(join(dist, asset))).byteLength;
  total += bytes;
  console.log(`${basename(asset).padEnd(44)} ${(bytes / 1024).toFixed(2)} KiB`);
}

console.log(`initial JavaScript gzip total: ${(total / 1024).toFixed(2)} KiB (limit 500.00 KiB)`);
if (total >= limit) {
  console.error("bundle budget exceeded");
  process.exit(1);
}

// 对比前端 frontend/src/lib/tagNorm.ts 与后端 internal/store/tag_norm.go 的简繁字对表，
// 防止两份手维护的表漂移。挂在 npm run lint 链中。
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

function extractPairs(filePath, pattern, label) {
  const src = readFileSync(filePath, "utf8");
  const m = src.match(pattern);
  if (!m) {
    console.error(`[test:tag-norm] FAIL: ${label} 中未找到字对表定义`);
    process.exit(1);
  }
  return m[1].split(/\s+/).filter(Boolean);
}

const goPairs = extractPairs(
  path.join(root, "internal", "store", "tag_norm.go"),
  /tagNormSimpTradPairs = `([^`]*)`/,
  "internal/store/tag_norm.go"
);
const tsPairs = extractPairs(
  path.join(root, "frontend", "src", "lib", "tagNorm.ts"),
  /TAG_NORM_SIMP_TRAD_PAIRS = `([^`]*)`/,
  "frontend/src/lib/tagNorm.ts"
);

if (goPairs.length !== tsPairs.length) {
  console.error(
    `[test:tag-norm] FAIL: 字对数不一致 —— 后端 ${goPairs.length} 对, 前端 ${tsPairs.length} 对`
  );
  process.exit(1);
}

const diffs = [];
for (let i = 0; i < goPairs.length; i++) {
  if (goPairs[i] !== tsPairs[i]) diffs.push(`第 ${i + 1} 对: 后端 ${goPairs[i]} != 前端 ${tsPairs[i]}`);
}
if (diffs.length > 0) {
  console.error(`[test:tag-norm] FAIL: ${diffs.length} 处不一致:\n  ${diffs.slice(0, 10).join("\n  ")}`);
  process.exit(1);
}

console.log(`[test:tag-norm] OK: 前后端字对表一致（${goPairs.length} 对）`);

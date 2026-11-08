#!/usr/bin/env node
/**
 * 文档与注释门禁：
 * 1. 必需文档存在，且不出现「未验证 / 已知不一致 / 已知缺口」这类未收口的小节；
 * 2. 文档不互相引用（目录树由 AGENTS.md 统一维护）；
 * 3. 代码注释不引用文档、不写历史叙事；
 * 3b. 文档本身不写历史叙事（AGENTS.md 除外：它是规则的正文，必须逐字列出这些词）；
 * 4. 文档里的 `路径:行号` 引用必须能解析到文件，且行号不越界；
 * 5. AGENTS.md 的文档地图只指向真实存在的文件；
 * 6. 源码文件不被 .gitignore 吞掉（忽略规则不锚定会命中源码目录）。
 *
 * 退出码 1 表示有违规；输出逐条列出文件与行号。
 */
import { execSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const DOCS = [
  'AGENTS.md',
  'README.md',
  'docs/product/REQUIREMENTS.md',
  'docs/tech/ARCHITECTURE.md',
  'docs/tech/SERVER.md',
  'docs/tech/APP.md',
  'docs/tech/ADMIN.md',
];

const errors = [];
const fail = (msg) => errors.push(msg);
const rel = (p) => path.relative(root, p) || p;
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const exists = (p) => fs.existsSync(path.join(root, p));

// ---------- 1. 必需文档与未收口小节 ----------
for (const doc of DOCS) {
  if (!exists(doc)) {
    fail(`缺少文档：${doc}`);
    continue;
  }
  const text = read(doc);
  for (const phrase of ['未验证', '已知不一致', '已知缺口']) {
    if (text.includes(phrase)) fail(`${doc} 含未收口表述「${phrase}」`);
  }
}

// ---------- 2. 文档不互相引用 ----------
for (const doc of DOCS) {
  if (!exists(doc)) continue;
  const lines = read(doc).split('\n');
  lines.forEach((line, i) => {
    if (/见\s*`?docs\/[A-Za-z]/.test(line) && !line.includes('AGENTS.md')) {
      fail(`${doc}:${i + 1} 跨文档引用：${line.trim().slice(0, 80)}`);
    }
  });
}

// ---------- 3. 代码注释：不引用文档、不写历史 ----------
const SCAN_DIRS = ['server', 'app', 'admin/src', 'admin/scripts', '.github'];
const SCAN_EXT = new Set(['.go', '.kt', '.kts', '.ts', '.vue', '.mjs', '.sql', '.yml', '.yaml']);
const HISTORY_AND_DOC = [
  /PRODUCT「/,
  /ARCHITECTURE「/,
  /SERVER「/,
  /APP\.md/,
  /ADMIN\.md/,
  /DEVELOPMENT「/,
  /(?:DEVELOPMENT|PRODUCT|INTEGRATIONS)\.md/,
  /DESIGN\.md/,
  /PAGES\.md/,
  /docs\/(tech|product)\//,
  /此前/,
  /原先/,
  /曾经/,
  /改回/,
  /上一版/,
  /旧版/,
  /旧 APP/,
  /老客户端/,
  /替代旧/,
  /取代旧/,
  /历史遗留/,
  /本次改动/,
  /已废弃/,
  /迁移自/,
  // 「已移除」本身可以是当前事实（如「已移除模型 X」的提示），只拦墓碑句式「已移除 A，改用 B」
  /已移除[^，。\n]{0,20}(?:改用|替换为)/,
];

const SKIP_NAMES = new Set(['node_modules', 'build', '.gradle', '.git', '.cache', '.gocache', '.pnpm-store', '.dsh', '.impeccable', '.verify', 'demo', 'bin', 'assets']);
const SKIP_PREFIX = ['server/data', 'app/build', 'admin/dist', 'server/internal/webui/dist', 'docs/dev'];

const skipped = (p) => SKIP_NAMES.has(path.basename(p)) || SKIP_PREFIX.some((pre) => p === pre || p.startsWith(pre + '/'));

function walk(dir, out) {
  if (!fs.existsSync(path.join(root, dir))) return;
  for (const entry of fs.readdirSync(path.join(root, dir), { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (skipped(p)) continue;
    if (entry.isDirectory()) walk(p, out);
    else out.push(p);
  }
}

const files = [];
for (const dir of SCAN_DIRS) walk(dir, files);
// 无扩展名的构建入口也属于注释门禁范围：CI 注释里曾残留指向已删除文档的引用
const EXTRA_FILES = ['Makefile', 'Dockerfile', 'docker-compose.yml', 'docker-entrypoint.sh'];
for (const f of EXTRA_FILES) if (exists(f)) files.push(f);
for (const f of files) {
  if (!SCAN_EXT.has(path.extname(f)) && !EXTRA_FILES.includes(f)) continue;
  const lines = fs.readFileSync(path.join(root, f), 'utf8').split('\n');
  lines.forEach((line, i) => {
    for (const re of HISTORY_AND_DOC) {
      if (re.test(line)) {
        fail(`${f}:${i + 1} 注释含文档引用/历史叙事（${re}）：${line.trim().slice(0, 90)}`);
        break;
      }
    }
  });
}

// ---------- 3b. 文档里的历史叙事 ----------
// 文档只有「现在是什么」一种时态：出现「此前 / 已废弃 / 没改之前」这类叙述即失败。
// AGENTS.md 不参与：它要把这些词逐字列出来当规则。
const DOC_HISTORY = [
  /此前/, /原先/, /曾经/, /旧版/, /上一版/, /老客户端/, /旧 APP/, /已废弃/,
  /迁移自/, /本次改动/, /历史遗留/, /改回/, /替代旧/, /取代旧/, /以前/, /改为/, /改成/, /新增了/, /修复了/,
];
for (const doc of DOCS) {
  if (doc === 'AGENTS.md' || !exists(doc)) continue;
  read(doc).split('\n').forEach((line, i) => {
    for (const re of DOC_HISTORY) {
      if (re.test(line)) {
        fail(`${doc}:${i + 1} 文档含历史叙事（${re}）：${line.trim().slice(0, 90)}`);
        break;
      }
    }
  });
}

// ---------- 4. `路径:行号` 引用解析 ----------
const INDEX = new Map();
for (const dir of ['.']) {
  const abs = path.join(root, dir);
  if (!fs.existsSync(abs)) continue;
  const stack = [dir];
  while (stack.length) {
    const cur = stack.pop();
    const absCur = path.join(root, cur);
    for (const entry of fs.readdirSync(absCur, { withFileTypes: true })) {
      const p = cur === '.' ? entry.name : path.join(cur, entry.name);
      if (skipped(p)) continue;
      if (entry.isDirectory()) stack.push(p);
      else {
        const name = path.basename(p);
        if (!INDEX.has(name)) INDEX.set(name, []);
        if (!INDEX.get(name).includes(p)) INDEX.get(name).push(p);
      }
    }
  }
}

const REF_RE = /`([A-Za-z0-9_./-]+\.(?:go|kt|kts|ts|vue|sql|yml|yaml|mjs|json|sh|mod)|Makefile|Dockerfile):(\d+)(?:-(\d+))?`/g;
const resolveRef = (p) => {
  const candidates = [p, `server/internal/${p}`, `server/${p}`, `app/${p}`, `admin/${p}`, `admin/src/${p}`, `server/internal/store/migrations/${p}`];
  for (const c of candidates) if (exists(c)) return c;
  if (p.includes('/')) return null;
  const byName = INDEX.get(path.basename(p)) || [];
  return byName.length === 1 ? byName[0] : null;
};

let refCount = 0;
for (const doc of DOCS) {
  if (!exists(doc)) continue;
  const lines = read(doc).split('\n');
  lines.forEach((line, i) => {
    for (const m of line.matchAll(REF_RE)) {
      refCount += 1;
      const [, p, start, end] = m;
      const real = resolveRef(p);
      if (!real) {
        fail(`${doc}:${i + 1} 引用文件不存在：${p}`);
        continue;
      }
      const text = fs.readFileSync(path.join(root, real), 'utf8');
      const total = text.split('\n').length - (text.endsWith('\n') ? 1 : 0);
      const max = Number(end || start);
      if (max > total) fail(`${doc}:${i + 1} 引用越界：${real}:${start}${end ? `-${end}` : ''}（共 ${total} 行）`);
    }
  });
}

// ---------- 5. AGENTS 文档地图指向真实文件 ----------
if (exists('AGENTS.md')) {
  const map = read('AGENTS.md');
  for (const m of map.matchAll(/`(docs\/[A-Za-z0-9_./-]+\.md)`/g)) {
    // docs/dev/ 是不入库的临时工作区（计划、审核交接），文件按需生成，不参与这条校验
    if (m[1].startsWith('docs/dev/')) continue;
    if (!exists(m[1])) fail(`AGENTS.md 文档地图指向不存在的文件：${m[1]}`);
  }
}

// ---------- 6. 源码不被 .gitignore 吞掉 ----------
// 忽略规则不锚定时会命中源码目录：`data/` 曾连 app/.../appnative/data/ 一起忽略，
// 15 个文件静默从提交里消失。这里逐个问 git：现有源码文件有没有被忽略。
const SRC_ROOTS = ['server', 'app', 'admin/src', 'admin/scripts', 'scripts', '.github'];
const SRC_RE = /\.(go|kt|kts|ts|vue|mjs|sql|ya?ml|json)$/;  // json 也算：go:embed 的数据文件缺了服务端编不过
const srcFiles = [];
for (const dir of SRC_ROOTS) walk(dir, srcFiles);
// 运行时数据目录（server/data*、根 data/）本来就该忽略，不算源码
const isRuntimeData = (f) => /^server\/data[^/]*\//.test(f) || /^data\//.test(f);
const srcPaths = srcFiles.filter((f) => SRC_RE.test(f) && !isRuntimeData(f));
if (srcPaths.length) {
  try {
    // exit 0 = 有文件被忽略（输出命中的路径）；exit 1 = 一个都没被忽略，正是期望结果
    const out = execSync('git check-ignore --stdin', {
      cwd: root,
      input: srcPaths.join('\n'),
      encoding: 'utf8',
      maxBuffer: 16 << 20,
    });
    const ignored = out.split('\n').filter(Boolean);
    if (ignored.length) fail(`.gitignore 吞掉了 ${ignored.length} 个源码文件：${ignored.slice(0, 5).join('、')}`);
  } catch (e) {
    if (e.status !== 1) fail(`.gitignore 检查失败：${e.message}`);
  }
}

// ---------- 输出 ----------
if (errors.length) {
  console.error(`✗ 文档/注释门禁未通过（${errors.length} 项）：`);
  for (const e of errors) console.error('  - ' + e);
  process.exit(1);
}
console.log(`✓ 文档/注释门禁通过：${DOCS.length} 份文档、${refCount} 处路径:行号引用`);

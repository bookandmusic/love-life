#!/usr/bin/env node
/**
 * 样式守卫：`pnpm guard`（verify 内含）。只扫嫁接的业务代码，模板自身的
 * 布局 / 主题 / 组件不扫（它们有自己的 token 体系）。
 *
 * 口径（以 src/tokens.ts 的令牌为唯一来源）：
 * 1. 颜色只允许 var(--token) 引用令牌：<style> 块与静态内联里不允许裸色值 / 色函数；
 * 2. font-size 必须走排版阶梯（var(--text-*)）；组件内部几何尺寸（图表高度等）允许 px；
 * 3. `var(--x)` 的 x 必须在 src/tokens.ts 的 cssVars 白名单里；
 * 4. 禁止 window.confirm / alert / prompt（后台统一确认弹窗 + toast）。
 *
 * 豁免：动态 :style 绑定是 JS 计算值（如图表柱高），不做静态扫描，但不得写颜色（评审口径）。
 * UnoCSS 原子类里携带的方括号色值（如 text-[rgb(...)]）只允许出现在定制组件的既有实现里，
 * 新代码一律走令牌 / 主题变量——评审时注意，脚本不拦原子类。
 */
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import process from 'node:process';

const root = process.cwd();

// ---- 扫描范围：嫁接的业务代码 ----
const TARGETS = [
  'src/api',
  'src/lib',
  'src/views',
  'src/components/custom',
  'src/components/SectionHeader.vue',
  'src/components/StatCard.vue',
  'src/components/ConfirmDialog.vue',
  'src/service/api/auth.ts',
  'src/store/modules/auth/index.ts',
  'src/store/modules/auth/shared.ts'
];

// ---- 令牌白名单：只取自 src/tokens.ts 的 buildCssVars 返回对象（唯一来源）----
const tokensSrc = readFileSync(join(root, 'src/tokens.ts'), 'utf8');
const buildCssVarsBody = tokensSrc.match(/export function buildCssVars[\s\S]*?\n}/)?.[0] ?? '';
if (!buildCssVarsBody) {
  console.error('[guard] 未找到 buildCssVars，tokens.ts 结构变动，请检查');
  process.exit(1);
}
const declaredKeys = [...buildCssVarsBody.matchAll(/'(--[a-z0-9-]+)':/g)].map(m => m[1]);
const varNames = new Set(declaredKeys);
// 键数精确一致：声明键数必须等于函数体内出现的 '--*' 字面量数（漏解析说明结构被改坏）
const literalKeys = new Set([...buildCssVarsBody.matchAll(/'(--[a-z0-9-]+)'/g)].map(m => m[1]));
if (!varNames.size || varNames.size !== literalKeys.size) {
  console.error(`[guard] buildCssVars 键解析不一致：声明 ${varNames.size} 个 / 字面量 ${literalKeys.size} 个，请检查 tokens.ts`);
  process.exit(1);
}

const violations = [];

const STYLE_BLOCK = /<style[^>]*>([\s\S]*?)<\/style>/g;
const HEX = /#[0-9a-fA-F]{3,8}\b/;
const FUNC_COLOR = /\b(?:rgba?|hsla?)\(/i;
// 字号必须走排版阶梯（--text-*）；组件内部几何尺寸（图表高度、头像框等）允许 px
const BARE_PX_FONTSIZE = /font-size\s*:\s*(?!\s*var\()\s*\d+(?:\.\d+)?px/;
const VAR_REF = /var\(\s*(--[a-z0-9-]+)\s*[),]/g;
const NATIVE_DIALOG = /\bwindow\.(?:confirm|alert|prompt)\s*\(/;

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    const st = statSync(p);
    if (st.isDirectory()) walk(p);
    else if (/\.(vue|css|ts)$/.test(name)) check(p);
  }
}

function check(file) {
  const text = readFileSync(file, 'utf8');
  const rel = relative(root, file);

  for (const m of text.matchAll(new RegExp(NATIVE_DIALOG, 'g'))) {
    violations.push(`${rel}: 禁用 window.${m[1]}() —— 用确认弹窗组件 / toast`);
  }

  const styleTexts = [];
  for (const m of text.matchAll(STYLE_BLOCK)) styleTexts.push({ body: m[1], at: rel });
  for (const m of text.matchAll(/(?<!:)\bstyle="([^"]*)"/g)) styleTexts.push({ body: m[1], at: `${rel}:内联` });

  for (const { body, at } of styleTexts) {
    const code = body.replace(/\/\*[\s\S]*?\*\//g, '');
    if (HEX.test(code)) violations.push(`${at}: <style> 出现裸色值 ${code.match(HEX)[0]} —— 用 var(--…) 令牌`);
    if (FUNC_COLOR.test(code)) violations.push(`${at}: <style> 出现 rgba/hsl 函数色 —— 用 var(--…) 令牌`);
    if (BARE_PX_FONTSIZE.test(code)) violations.push(`${at}: font-size 出现裸 px —— 用 var(--text-*) 排版阶梯`);
    for (const v of code.matchAll(VAR_REF)) {
      if (!varNames.has(v[1])) {
        violations.push(`${at}: var(${v[1]}) 未在 tokens.ts 定义 —— 后台令牌只认 src/tokens.ts`);
      }
    }
  }
}

// 反向检查的范围是全部 src（含全局 css/scss），但跳过 tokens.ts 自身的声明行
function collectSourceText(dir) {
  const chunks = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    const st = statSync(p);
    if (st.isDirectory()) chunks.push(collectSourceText(p));
    else if (/\.(vue|css|scss|ts)$/.test(name) && !p.endsWith('src/tokens.ts')) chunks.push(readFileSync(p, 'utf8'));
  }
  return chunks.join('\n');
}

for (const target of TARGETS) {
  const p = join(root, target);
  try {
    const st = statSync(p);
    if (st.isDirectory()) walk(p);
    else check(p);
  } catch {
    console.error(`[guard] 扫描目标不存在: ${target}`);
    process.exit(1);
  }
}

// ---- 反向检查：每个声明令牌至少被 src/ 内一处 var(--x) 引用 ----
const srcText = collectSourceText(join(root, 'src'));
const usedVars = new Set([...srcText.matchAll(VAR_REF)].map(m => m[1]));
for (const name of varNames) {
  if (!usedVars.has(name)) {
    violations.push(`src/tokens.ts: ${name} 声明后无任何 var(${name}) 引用 —— 删除死令牌或补使用`);
  }
}

if (violations.length) {
  console.error(`[guard] ${violations.length} 处违规：`);
  for (const v of violations) console.error('  - ' + v);
  process.exit(1);
}
console.log('[guard] 样式守卫通过：无裸色值 / 裸 font-size px / 未定义 var(--) / 无死令牌 / 原生弹窗');

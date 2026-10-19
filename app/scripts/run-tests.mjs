// APP 测试入口：跑用例 + 守住「该跑的用例一个都没少」。
//
// 为什么要这层：`node --test` 在两种情况下会**静默少跑**却仍退出 0——
//   ① glob 一个文件都没匹配到（改名/换后缀 → 整套测试消失）；
//   ② 显式传多个文件但 `--test` 写在文件列表之后（node 只把第一个当入口脚本执行，其余忽略）。
// 故这里：用例文件由**目录扫描**得到（不用 shell glob、不手写清单），并把 runner 报告里
// 的用例数与文件数一起核对；少于已登记的数量即失败，避免「测试还在、只是没跑」。
import { spawnSync } from 'node:child_process'
import { readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

// 已登记的用例数：新增用例时抬高它。这不是「好看的数字」，而是「至少跑过这么多」的下限——
// 调低它必须是一个显式动作，不能因为文件没被找到而悄悄变少。
const MIN_TESTS = 31

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const utilsDir = join(root, 'src', 'utils')
const files = readdirSync(utilsDir)
  .filter((f) => f.endsWith('.test.mjs'))
  .sort()
  .map((f) => join('src', 'utils', f))

if (files.length === 0) {
  console.error('✖ 没找到任何 .test.mjs 用例文件——测试不可能全绿，请检查目录')
  process.exit(1)
}

// --test 必须在文件列表之前（见文件头注释②）
// --test-reporter=spec 必须显式钉住：Node 默认报告器随版本变（22 走 TAP、24 走 spec），
// 下面按「ℹ tests N」解析汇总行，不钉就会在 CI 的 Node 22 上解析成 null 而误报失败。
const run = spawnSync(
  process.execPath,
  ['--experimental-strip-types', '--test', '--test-reporter=spec', ...files],
  { cwd: root, encoding: 'utf8' },
)
process.stdout.write(run.stdout)
process.stderr.write(run.stderr)

// 只认汇总行（形如「ℹ tests 9」）：锚定行首的 ℹ 前缀，且要求整行只有该标签与数字。
// 逐条用例行是「✔ <用例名> (耗时)」——用例名由测试作者任意书写，
// 若把汇总解析放宽到「找第一个 tests N」，一个名为 "…\ntests 999" 的用例就可能顶替真实计数。
const summary = (label) => {
  // 模板字符串会吞掉 \d（变成字面量 d），故用 String.raw 写模式
  const m = run.stdout.match(new RegExp(String.raw`^\u2139 ${label} (\d+)$`, 'm'))
  return m ? Number(m[1]) : null
}
const tests = summary('tests')
const fail = summary('fail')

if (run.status !== 0 || fail !== 0) {
  console.error(`\n✖ 测试未通过（status=${run.status} fail=${fail}）`)
  process.exit(1)
}
if (tests === null || tests < MIN_TESTS) {
  console.error(
    `\n✖ 只跑了 ${tests ?? '?'} 个用例（下限 ${MIN_TESTS}）：` +
      '通常是用例文件没被找到或没被执行——确属有意减少请同步改 MIN_TESTS',
  )
  process.exit(1)
}
console.log(`\n✔ 测试通过（${tests} 个用例 / ${files.length} 个文件，下限 ${MIN_TESTS}）`)

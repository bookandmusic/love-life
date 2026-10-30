// 登录凭据的安全存储（勾选「记住登录」才保存）。
//
// 存储位置：系统安全存储（Android Keystore / iOS Keychain），
// 经 tauri-plugin-keystore。**绝不写 localStorage**——明文密码可被 XSS 与数据目录读取。
//
// 插件契约（对照 crate 2.1.0-alpha.2 的 src/models.rs 与 README）：
//   store(key, value)             按键存一条
//   retrieve(key) -> string|null   取一条，无则 null
//   remove(key)                   删一条
//   每次 store/retrieve 都会触发系统生物识别验证
//
// 插件限制（决定降级行为）：
// - 仅 Android / iOS；桌面与浏览器不支持
// - 要求设备已设置生物识别，未设置时调用抛错
// - 仅发布过 alpha 版本，故 Cargo.toml / package.json 都钉死版本号
//
// 降级策略：不可用时不保存密码，免登录只靠 refresh token（见 PRODUCT「记住登录」）。
import { isTauri } from '@tauri-apps/api/core'

export interface Credentials {
  username: string
  password: string
}

const KEY_USER = 'll.username'
const KEY_PASS = 'll.password'

// 浏览器开发环境的内存副本（刷新即失效，仅用于本地调试表单预填）
let memCache: Credentials | null = null

// 动态引入：非 Tauri 环境（浏览器开发）里该包不可用，
// 放在函数内 try/catch 中加载，失败即走内存降级。
async function keystore() {
  return await import('@impierce/tauri-plugin-keystore')
}

/** 保存凭据；失败返回 false（记住登录不是关键路径，不阻断登录流程） */
export async function saveCredentials(username: string, password: string): Promise<boolean> {
  if (!isTauri()) {
    memCache = { username, password }
    return false
  }
  try {
    const { store } = await keystore()
    await store(KEY_USER, username)
    await store(KEY_PASS, password)
    return true
  } catch {
    // 设备未设生物识别 / 插件未注册 / 用户取消验证：都走这里
    return false
  }
}

/** 读取凭据；无凭据或不可用时返回 null */
export async function loadCredentials(): Promise<Credentials | null> {
  if (!isTauri()) return memCache
  try {
    const { retrieve } = await keystore()
    const username = await retrieve(KEY_USER)
    const password = await retrieve(KEY_PASS)
    if (!username || !password) return null
    return { username, password }
  } catch {
    return null
  }
}

/** 清除凭据（退出登录、取消「记住登录」时调用） */
export async function clearCredentials(): Promise<void> {
  memCache = null
  if (!isTauri()) return
  try {
    const { remove } = await keystore()
    await remove(KEY_USER)
    await remove(KEY_PASS)
  } catch {
    /* 清除失败不阻断登出：本地 token 已清，凭据残留最多导致下次预填 */
  }
}
// 对话历史本地缓存（AP2）：服务端为权威存储，indexedDB 仅加速回显。
// 按 uid + 会话 id 分键（多会话）；断网时展示对应会话最近一次拉取的历史。
const DB_NAME = 'love-life'
const STORE = 'chat_history'
const DB_VERSION = 1

// 上限与淘汰：本地缓存只做「加速回显」，服务端才是权威存储，超出上限按**最近一次写入时间**
// 淘汰最旧的（不是访问时间——离线读缓存会刷新访问序，那会让常用会话永远不淘汰）。
// PRODUCT「设置 · 本机存储与缓存」：本地缓存自动管理（容量上限 + 按使用情况自动淘汰旧内容），
// 设置页不提供手动清理入口。
const MAX_CACHED_SESSIONS = 30

// 缓存条目：写入时间用于淘汰；旧版本直接存 history 本身，读取时按形状兼容。
interface CacheEntry {
  savedAt: number
  history: unknown
}

// 按写入时间从旧到新挑出要淘汰的键
function pickEvictions(entries: Array<{ key: string; savedAt: number }>, max: number): string[] {
  if (entries.length <= max) return []
  return [...entries]
    .sort((a, b) => a.savedAt - b.savedAt)
    .slice(0, entries.length - max)
    .map((e) => e.key)
}

let dbPromise: Promise<IDBDatabase> | null = null

function openDB(): Promise<IDBDatabase> {
  if (!dbPromise) {
    dbPromise = new Promise((resolve, reject) => {
      const req = indexedDB.open(DB_NAME, DB_VERSION)
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains(STORE)) {
          req.result.createObjectStore(STORE)
        }
      }
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => reject(req.error)
    })
  }
  return dbPromise
}

function request<T>(mode: IDBTransactionMode, run: (store: IDBObjectStore) => IDBRequest): Promise<T> {
  return openDB().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const tx = db.transaction(STORE, mode)
        const req = run(tx.objectStore(STORE))
        req.onsuccess = () => resolve(req.result as T)
        req.onerror = () => reject(req.error)
      }),
  )
}

export async function loadCachedHistory(uid: number, sessionId: number): Promise<unknown | null> {
  try {
    const val = await request<unknown>('readonly', (s) => s.get(`history:${uid}:${sessionId}`))
    if (val == null) return null
    // 兼容旧条目（直接存 history 本身，无 savedAt 包装）
    if (typeof val === 'object' && val !== null && 'savedAt' in val && 'history' in val) {
      return (val as CacheEntry).history
    }
    return val
  } catch {
    return null
  }
}

export async function saveCachedHistory(uid: number, sessionId: number, history: unknown): Promise<void> {
  try {
    const key = `history:${uid}:${sessionId}`
    const entry: CacheEntry = { savedAt: Date.now(), history }
    await request('readwrite', (s) => s.put(entry, key))
    await evictOverflow(uid)
  } catch {
    // 缓存失败不影响主流程（隐私模式等场景 indexedDB 可能不可用）
  }
}

// evictOverflow 只统计**本用户**的会话条目：多账号共用同一个 objectStore，
// 淘汰另一个账号的缓存既越权也会误伤。
async function evictOverflow(uid: number): Promise<void> {
  const prefix = `history:${uid}:`
  const entries = await scanPrefix(prefix)
  const drop = pickEvictions(entries, MAX_CACHED_SESSIONS)
  if (!drop.length) return
  await deleteKeys(drop)
}

// deleteKeys 一个事务里删掉多个键（回调式 API 的完成以事务 oncomplete 为准）。
function deleteKeys(keys: string[]): Promise<void> {
  return openDB().then(
    (db) =>
      new Promise((resolve, reject) => {
        const tx = db.transaction(STORE, 'readwrite')
        const store = tx.objectStore(STORE)
        for (const k of keys) store.delete(k)
        tx.oncomplete = () => resolve()
        tx.onerror = () => reject(tx.error)
      }),
  )
}

// scanPrefix 用游标遍历出该前缀下的全部键与写入时间（游标 onsuccess 逐行触发，
// 故不能复用 request 的单次成功语义）。
function scanPrefix(prefix: string): Promise<Array<{ key: string; savedAt: number }>> {
  return openDB().then(
    (db) =>
      new Promise((resolve, reject) => {
        const tx = db.transaction(STORE, 'readonly')
        const out: Array<{ key: string; savedAt: number }> = []
        const req = tx.objectStore(STORE).openCursor()
        req.onsuccess = () => {
          const cur = req.result
          if (!cur) {
            resolve(out)
            return
          }
          const k = String(cur.key)
          if (k.startsWith(prefix)) {
            const v = cur.value as Partial<CacheEntry> | null
            // 旧条目无 savedAt：按 0 处理，优先淘汰
            out.push({ key: k, savedAt: typeof v?.savedAt === 'number' ? v.savedAt : 0 })
          }
          cur.continue()
        }
        req.onerror = () => reject(req.error)
      }),
  )
}

// 对话历史本地缓存（AP2）：服务端为权威存储，indexedDB 仅加速回显。
// 按 uid + 会话 id 分键（多会话）；断网时展示对应会话最近一次拉取的历史。
const DB_NAME = 'love-life'
const STORE = 'chat_history'
const DB_VERSION = 1

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
    return val ?? null
  } catch {
    return null
  }
}

export async function saveCachedHistory(uid: number, sessionId: number, history: unknown): Promise<void> {
  try {
    await request('readwrite', (s) => s.put(history, `history:${uid}:${sessionId}`))
  } catch {
    // 缓存失败不影响主流程（隐私模式等场景 indexedDB 可能不可用）
  }
}

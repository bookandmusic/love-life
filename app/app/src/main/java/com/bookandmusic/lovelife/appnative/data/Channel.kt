package com.bookandmusic.lovelife.appnative.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import okhttp3.OkHttpClient
import okhttp3.Request
import java.time.Duration
import java.time.Instant

/**
 * 服务地址通道：**按权重统一选路**。
 *
 * 1. 内置地址权重恒 [BUILTIN_WEIGHT]（后端下发 `builtin_weight`，未给则用本常量）——
 *    后端改不了它，避免配错一个值把 APP 锁死在不可达地址。
 * 2. 拉 `GET /api/v1/channels` 拿全部后端侧通道（绿联 + 备用地址，各带权重）。
 * 3. 内置地址与之合并，按权重**降序**排序，依次探活（超时 5 秒），第一个通的即当前通道。
 * 4. 全不通 → `base()` 回落内置地址（错误照常上报，不静默）。
 *
 * 只有绿联通道带 cookie（中转自己的会话凭据，后端下发）；其余通道 cookie 为空，
 * 请求也就不发 Cookie 头。
 *
 * **对话中不切通道**：一旦开始收帧，断了就是断了（标「已中断」），
 * 下次发消息前才重新选路——否则同一轮会发两遍。
 */
object Channel {
    /** 内置地址权重：与后端 `channelWeightBuiltin` 同值；后端没下发时用它兜底 */
    const val BUILTIN_WEIGHT = 100

    enum class Kind { UGLINK, BACKUP, BUILTIN }

    /**
     * 一条候选通道（选路的基本单位）。
     * 纯数据 + 纯排序函数，便于单测覆盖「权重降序」这条契约。
     */
    data class Candidate(
        val kind: Kind,
        val url: String,
        val weight: Int,
        val cookie: String = "",
        val expiresAt: Long = 0,
    )

    @Volatile private var active: Candidate? = null
    @Volatile private var lastWatchRefresh: Long = 0
    @Volatile private var lastForegroundRefresh: Long = 0
    @Volatile private var inflight: Deferred<Unit>? = null

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val resolveMutex = Mutex()
    // 探活超时 5 秒：2 秒对「前置 CDN + 冷 TLS」太紧——同一域名冷连常在 1–2s 量级，
    // 移动网首连还更慢，结果就是**高优先级通道被误判不可用**、退到低优先级那条。
    // 代价是全通道都不可用时多等几秒，但那只发生在真断网时。
    private val probeHttp = OkHttpClient.Builder().callTimeout(Duration.ofSeconds(5)).build()

    fun kindValue(): Kind = active?.kind ?: Kind.BUILTIN
    fun base(): String = active?.url?.takeIf { it.isNotEmpty() } ?: ApiClient.builtinBase()

    /**
     * 把服务端下发的媒体地址补成绝对地址。
     * 服务端只给相对路径（`/api/v1/media/file/13?v=thumb&exp=…&sig=…`），Coil 拿相对路径加载不出来；
     * 时光/相册那边是数据层各自拼的，结果卡走的是这里统一拼，别再各写一份。
     */
    fun absolute(url: String): String = absoluteUrl(base(), url)
    fun cookieValue(): String = active?.cookie.orEmpty()

    /** 是否已解析出可用通道（false = base() 在回落内置地址） */
    fun isResolved(): Boolean = active != null

    /** 保证已解析：没解析过就先解析（进程重建后 Channel 是空的） */
    suspend fun ensureResolved() {
        if (!isResolved()) refreshChannel().await()
    }

    suspend fun init() { refreshChannel().await() }

    /** 绿联凭据到期前 60 秒主动续期；其余通道零开销 */
    suspend fun ensureFresh() {
        val cur = active ?: return
        if (cur.kind != Kind.UGLINK || cur.expiresAt == 0L) return
        if (System.currentTimeMillis() >= cur.expiresAt - 60_000) refreshChannel().await()
    }

    /**
     * 标记当前通道失败（**不立即切换**）：置空 → 下次 ensureResolved 重新选路。
     * 立刻换一条的话，对话进行到一半会把同一轮发两遍。
     */
    fun markFailed() {
        active = null
    }

    fun refreshChannel(): Deferred<Unit> = synchronized(this) {
        inflight?.let { return it }
        val d = scope.async { resolve() }
        inflight = d
        d.invokeOnCompletion { synchronized(this) { if (inflight === d) inflight = null } }
        d
    }

    /**
     * 合并候选并按权重降序排（纯函数，可单测）。
     * 同权重保持原顺序（稳定）：内置排最前，故同权重时优先直连。
     */
    internal fun merged(
        builtin: String,
        builtinWeight: Int,
        backend: List<Candidate>,
    ): List<Candidate> {
        val list = buildList {
            builtin.trimEnd('/').takeIf { it.isNotEmpty() }?.let {
                add(Candidate(Kind.BUILTIN, it, builtinWeight))
            }
            addAll(backend)
        }
        return list.sortedByDescending { it.weight }
    }

    private suspend fun resolve() = resolveMutex.withLock {
        val backend = fetchCandidates()
        val candidates = merged(ApiClient.builtinBase(), backend.second, backend.first)
        for (c in candidates) {
            if (probe(c.url) < Long.MAX_VALUE) {
                active = c
                return@withLock
            }
        }
        // 全部探活失败：**保留上次可用的通道**，不要清空。
        // 清空会让 base() 退回内置地址（通常是私网），把「暂时探活失败」放大成「全不可用」。
        // 真不可用时下一次请求会失败 → markFailed() → 置空 → 再解析，语义仍然收敛。
    }

    /** 拉统一通道列表；返回（候选, 内置权重）。取不到 → 空列表 + 默认权重。 */
    private suspend fun fetchCandidates(): Pair<List<Candidate>, Int> {
        val text = try {
            ApiClient.apiRaw("/api/v1/channels", "GET", null, retry = false)
        } catch (_: Exception) {
            null
        } ?: return emptyList<Candidate>() to BUILTIN_WEIGHT

        return try {
            val dto = ApiClient.json.decodeFromString(ChannelsDto.serializer(), text)
            val list = dto.items.mapNotNull { item ->
                val url = item.url?.trimEnd('/')?.takeIf { it.isNotEmpty() } ?: return@mapNotNull null
                Candidate(
                    kind = if (item.kind == "uglink") Kind.UGLINK else Kind.BACKUP,
                    url = url,
                    weight = item.weight,
                    // cookie 只随绿联通道来（其余通道后端不下发）
                    cookie = item.cookie.orEmpty(),
                    expiresAt = item.expiresAt?.let {
                        runCatching { Instant.parse(it).toEpochMilli() }.getOrDefault(0L)
                    } ?: 0L,
                )
            }
            list to (dto.builtin_weight ?: BUILTIN_WEIGHT)
        } catch (_: Exception) {
            emptyList<Candidate>() to BUILTIN_WEIGHT
        }
    }

    /** 探活：GET /api/v1/health，callTimeout 5 秒上限，返回往返毫秒；不可达返回 Long.MAX_VALUE */
    private suspend fun probe(base: String): Long {
        val started = System.currentTimeMillis()
        return try {
            probeHttp.newCall(
                Request.Builder().url(base.trimEnd('/') + "/api/v1/health").build()
            ).execute().use { if (!it.isSuccessful) Long.MAX_VALUE else System.currentTimeMillis() - started }
        } catch (_: Exception) {
            Long.MAX_VALUE
        }
    }

    /**
     * 回到前台时重新选路（60 秒去抖）。
     * 通道只在进程启动时解析一次，后台改了权重/加了地址就永远不生效——
     * 用户看到的是「改了配置但 APP 还是走旧通道」。重进前台刷一次最省事，
     * 且此时没有正在进行的流式对话，不违反「对话中不切通道」。
     */
    fun refreshOnForeground() {
        val now = System.currentTimeMillis()
        if (now - lastForegroundRefresh < 60_000) return
        lastForegroundRefresh = now
        refreshChannel()
    }

    /** 定时刷新（由前台 watcher 每 5 分钟驱动） */
    fun refreshOnNetworkChange() {
        val now = System.currentTimeMillis()
        if (now - lastWatchRefresh < 10_000) return
        lastWatchRefresh = now
        refreshChannel()
    }

    /** 前台定时探活循环；由 Activity 在 STARTED 状态驱动 */
    suspend fun watcherLoop() {
        while (true) {
            delay(5 * 60_000L)
            refreshOnNetworkChange()
        }
    }
}

/** 相对媒体地址 → 绝对地址；已是 http(s) 的原样返回（纯函数，便于单测）。 */
fun absoluteUrl(base: String, url: String): String {
    if (url.isBlank()) return url
    if (url.startsWith("http://") || url.startsWith("https://")) return url
    if (url.startsWith("//")) return "https:" + url
    return base.trimEnd('/') + "/" + url.trimStart('/')
}

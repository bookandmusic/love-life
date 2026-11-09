package com.bookandmusic.lovelife.appnative.ui

import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Audiotrack
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.IosShare
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.foundation.text.BasicTextField
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppActionSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMenuItem
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.ParagraphStyle
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextIndent
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.ChatRepository
import com.bookandmusic.lovelife.appnative.data.errText
import com.bookandmusic.lovelife.appnative.data.ChatState
import com.bookandmusic.lovelife.appnative.data.ChatState.Seg
import com.bookandmusic.lovelife.appnative.data.MediaRepository
import com.bookandmusic.lovelife.appnative.data.UiFence
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppChip
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppAvatar
import com.bookandmusic.lovelife.appnative.designsystem.atoms.brandTileShape
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadge
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadgeSize
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppPillButton
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppToast
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppTopBarAction
import kotlinx.coroutines.launch

/**
 * AI 对话页：
 * 顶栏（菜单 + 标题 + 设置）· 对话区（分段渲染：思考竖线行/工具灰容器/正文气泡）·
 * 输入区（chrome 底栏 + 发送/停止）· 空会话欢迎语 + chips。
 * 会话抽屉 / 引用条 /「+」面板都由本页挂接。
 */
@Composable
fun ChatScreen(
    contentPadding: androidx.compose.foundation.layout.PaddingValues,
    aiAvailable: Boolean,
    aiUnavailableReason: String,
    onBackToClassic: () -> Unit,
    onOpenSettings: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var boot by remember { mutableStateOf<ChatRepository.Bootstrap?>(null) }
    var messages by remember { mutableStateOf<List<ChatState.RenderMessage>>(emptyList()) }
    var input by remember { mutableStateOf("") }
    var streaming by remember { mutableStateOf(false) }
    // 当前流式轮 id（「停止生成」时 abort 用）：属于本页状态。文件级单例会跨会话残留——
    // 切换会话后按停止会 abort 上一轮的 turn。
    var currentTurnId by remember { mutableStateOf<String?>(null) }
    var sessionId by remember { mutableLongStateOf(0L) }
    var toast by remember { mutableStateOf<String?>(null) }
    // 媒体查看器（结果卡缩略图点开 → 复用全屏查看器；不跨模式跳转）
    var viewer by remember {
        mutableStateOf<Pair<List<com.bookandmusic.lovelife.appnative.data.PostMediaUi>, Int>?>(null)
    }
    // 引用：结果卡勾选 → 引用条 → 随下一条消息提交；仅当前会话内有效
    var quotes by remember { mutableStateOf<List<UiQuoted>>(emptyList()) }

    fun toggleQuote(q: UiQuoted) {
        quotes = if (quotes.any { it.record.id == q.record.id }) quotes.filterNot { it.record.id == q.record.id }
        else quotes + q
    }

    // 「+」面板：选媒体 / 快捷提问
    // 附件面板：照片 / 视频 / 音频（三选一，各自限定 mime）
    var showMediaMenu by remember { mutableStateOf(false) }
    // 「+」面板：五条快捷提问（点一下填进输入框，不直接发送）
    var showCommands by remember { mutableStateOf(false) }

    // 待发媒体：只记本机信息（uri + name/type/size），文件不上传——
    // 「选中即上传」会让没发出去的文件占着服务端磁盘（取消提案后成孤儿）。
    var pendingMedia by remember { mutableStateOf<List<PendingMedia>>(emptyList()) }
    // 提案 → 本轮待发媒体：确认执行那一刻才上传换 token，token 随 resolve 提交
    val cardMedia = remember { androidx.compose.runtime.mutableStateMapOf<Long, List<PendingMedia>>() }
    // 已发出、但还没有任何提案认领的附件：用户常常「先传图，下一句才说发布」，
    // 那批图属于上一条消息，提案在下一轮才出现——不带着走就会发出一条没有图动态。
    var unclaimedMedia by remember { mutableStateOf<List<PendingMedia>>(emptyList()) }
    val appContext = LocalContext.current
    // 附件统一走这条：只读本机元数据，不联网
    fun addMedia(uris: List<android.net.Uri>) {
        if (uris.isEmpty()) return
        val room = (9 - pendingMedia.size).coerceAtLeast(0)
        val added = uris.take(room).mapNotNull { uri ->
            runCatching { MediaRepository.localMeta(appContext, uri) }
                .onFailure { toast = errText(it, "读取文件失败") }
                .getOrNull()
                ?.let { m -> PendingMedia(uri = uri, name = m.name, mime = m.mime, size = m.size, kind = m.kind) }
        }
        pendingMedia = pendingMedia + added
    }
    val pickMedia = rememberLauncherForActivityResult(
        ActivityResultContracts.PickMultipleVisualMedia(9),
    ) { addMedia(it) }
    // 音频不在相册选择器里：走系统文件选择（audio/*，可多选）
    val pickAudio = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenMultipleDocuments(),
    ) { addMedia(it) }

    // 会话抽屉（仿 DeepSeek）：连续位移 0f=收起 / 1f=完全展开——
    // 布尔值只能「松手才开」，跟手拖拽必须有中间态
    var drawerFraction by remember { androidx.compose.runtime.mutableFloatStateOf(0f) }
    val panelWidthPx = with(androidx.compose.ui.platform.LocalDensity.current) {
        (windowWidthDp() * 0.82f).toPx()
    }
    val drawerScope = rememberCoroutineScope()
    fun settleDrawer(open: Boolean) {
        drawerScope.launch {
            androidx.compose.animation.core.animate(
                initialValue = drawerFraction,
                targetValue = if (open) 1f else 0f,
                animationSpec = androidx.compose.animation.core.tween(220),
            ) { v, _ -> drawerFraction = v }
        }
    }
    fun openDrawer() = settleDrawer(true)
    fun closeDrawer() = settleDrawer(false)
    var sessions by remember { mutableStateOf<List<ChatRepository.SessionInfo>>(emptyList()) }
    // 「选择对话」管理页（参考图 06）：多选 + 批量删除
    var manageOpen by remember { mutableStateOf(false) }
    // 系统返回键：先收「选择对话」管理页，再收抽屉；两层都收起后才轮到壳层（AI 模式是根页 → 系统退出应用）。
    BackHandler(enabled = manageOpen || drawerFraction > 0f) {
        if (manageOpen) manageOpen = false else closeDrawer()
    }
    var switching by remember { mutableStateOf(false) }

    fun loadSessions() {
        scope.launch {
            runCatching { ChatRepository.sessions() }.onSuccess { sessions = it }
        }
    }

    fun switchSession(id: Long) {
        // 流式期间切换：在途流的 session / turn.start 事件会把 sessionId、currentTurnId 写回旧会话，
        // 新会话的回复会落到上一轮上。所以这期间拒绝切换。
        if (streaming) {
            toast = "正在生成回复，先停止或稍候"
            return
        }
        if (switching) return
        switching = true
        // 换会话即换轮：旧 turn 的 abort 目标不能带到新会话
        currentTurnId = null
        scope.launch {
            try {
                sessionId = id
                val history = runCatching { ChatRepository.history(id) }.getOrDefault(emptyList())
                messages = history.map { ChatState.fromHistory(it) }
                closeDrawer()
            } finally {
                switching = false
            }
        }
    }

    fun newSession() {
        // 同 switchSession：流式期间新建，在途流仍会往旧会话/旧轮次写回
        if (streaming) {
            toast = "正在生成回复，先停止或稍候"
            return
        }
        sessionId = 0
        messages = emptyList()
        currentTurnId = null
        closeDrawer()
    }

    fun deleteSession(id: Long) {
        // 删当前会话等价于切走：流式期间拒绝，否则在途流仍会把 sessionId 写回已删除会话
        if (streaming && id == sessionId) {
            toast = "正在生成回复，先停止或稍候"
            return
        }
        scope.launch {
            runCatching { ChatRepository.deleteSession(id) }
                .onSuccess {
                    sessions = sessions.filter { it.id != id }
                    if (id == sessionId) newSession()
                }
                .onFailure { toast = errText(it, "删除失败") }
        }
    }
    fun togglePin(id: Long) {
        scope.launch {
            runCatching { ChatRepository.pinSession(id) }
                .onSuccess {
                    sessions = sessions.map {
                        if (it.id == id) it.copy(pinned = !it.pinned) else it
                    }.sortedWith(compareByDescending<ChatRepository.SessionInfo> { it.pinned })
                    toast = if (sessions.firstOrNull { it.id == id }?.pinned == true) "已置顶" else "已取消置顶"
                }
                .onFailure { toast = errText(it, "置顶失败") }
        }
    }
    fun deleteSessions(ids: List<Long>) {
        // 批量删除里含当前会话时与 switchSession 同理：流式期间拒绝
        if (streaming && sessionId in ids) {
            toast = "正在生成回复，先停止或稍候"
            return
        }
        scope.launch {
            var failed = 0
            ids.forEach { id ->
                runCatching { ChatRepository.deleteSession(id) }
                    .onSuccess {
                        sessions = sessions.filterNot { it.id == id }
                        if (id == sessionId) newSession()
                    }
                    .onFailure { failed += 1 }
            }
            if (failed > 0) toast = "有 $failed 个会话没删掉"
        }
    }
    // 当前会话是否有未过期的待确认卡（待确认期间不能切换或删除当前会话）
    val pendingLock = messages.any { m ->
        m.segs.any {
            it is Seg.Tool && it.card?.let { c ->
                ChatState.effectiveStatus(c.status, it.cardExpireAt) == "pending"
            } == true
        }
    }
    // 只用于「跟随滚动」：内容本身是快照状态（ChatState），重组不靠它。
    // 每来一帧就重启滚动副作用，把视口贴到底部。
    var scrollTick by remember { mutableIntStateOf(0) }
    // 当前用户（右侧头像用）：启动取一次，失败退化为无头像（不阻塞对话）
    var meState by remember { mutableStateOf<com.bookandmusic.lovelife.appnative.data.UserInfo?>(null) }
    // 真实头像：/space 的成员带签名 URL（用户信息里没有头像字段）
    var myAvatar by remember { mutableStateOf("") }
    LaunchedEffect(Unit) {
        meState = runCatching { com.bookandmusic.lovelife.appnative.data.AuthRepository.restore() }.getOrNull()
        val me = meState
        val space = runCatching { com.bookandmusic.lovelife.appnative.data.SpaceRepository.info() }.getOrNull()
        val url = space?.members?.firstOrNull { it.id == me?.id }?.avatar.orEmpty()
        if (url.isNotEmpty()) {
            myAvatar = com.bookandmusic.lovelife.appnative.data.Channel.base().trimEnd('/') + url
        }
    }
    val listState = rememberLazyListState()

    LaunchedEffect(Unit) {
        boot = runCatching { ChatRepository.bootstrap() }.getOrNull()
    }

    // 翻页消息：「再看几条」（同一查询条件推进偏移量——由模型按上下文继续取）
    fun sendMore() {
        if (streaming) return
        scope.launch {
            val text = "继续看更多"
            val userMsg = ChatState.RenderMessage(id = -System.nanoTime(), role = "user").apply {
                segs.add(Seg.Text(text, done = true))
            }
            val assistant = ChatState.RenderMessage(id = System.nanoTime(), role = "assistant")
            messages = messages + userMsg + assistant
            streaming = true
            try {
                ChatRepository.sendMessageStream(
                    sessionId = sessionId, text = text,
                    onEvent = { ev ->
                        when (ev.event) {
                            "session" -> sessionId = ChatState.parseJsonLong(ev.data, "session_id")
                            "turn.start" -> {
                                currentTurnId = ChatState.parseJsonString(ev.data, "turn_id")
                                val tid = currentTurnId.orEmpty()
                                userMsg.turnId = tid
                                assistant.turnId = tid
                            }
                            else -> ChatState.applyEvent(assistant, ev)
                        }
                        scrollTick++
                    },
                )
            } catch (e: Exception) {
                toast = "翻页失败"
            } finally {
                scrollTick++
                streaming = false
            }
        }
    }

    // 发一轮消息：输入框发送与「重新生成」复用同一条链路（媒体/引用各自由调用方给）
    fun sendTurn(
        text: String,
        turnMedia: List<PendingMedia>,
        quotedRefs: List<ChatRepository.QuoteRef>,
    ) {
        scope.launch {
            val userMsg = ChatState.RenderMessage(id = -System.nanoTime(), role = "user").apply {
                segs.add(Seg.Text(text, done = true))
            }
            val assistant = ChatState.RenderMessage(id = System.nanoTime(), role = "assistant")
            userMsg.quotes = quotedRefs.map { it.label }.filter { it.isNotBlank() }
            userMsg.mediaCount = turnMedia.size
            val rich = turnMedia.isNotEmpty() || quotedRefs.isNotEmpty()
            userMsg.hasRichInput = rich
            assistant.hasRichInput = rich
            messages = messages + userMsg + assistant
            streaming = true
            try {
                ChatRepository.sendMessageStream(
                    sessionId = sessionId,
                    text = text,
                    quoted = quotedRefs,
                    media = turnMedia.map { m ->
                        ChatRepository.AttachmentMeta(
                            name = m.name,
                            // kind 是 photo/video/audio，消息 media.type 要 image/video/audio 前缀
                            type = attachMime(m.kind, m.mime),
                            size = m.size,
                        )
                    },
                    onEvent = { ev ->
                        when (ev.event) {
                            "session" -> sessionId = ChatState.parseJsonLong(ev.data, "session_id")
                            "turn.start" -> {
                                currentTurnId = ChatState.parseJsonString(ev.data, "turn_id")
                                // 记住本轮 turn_id：历史重载之前也能对这条回复「重新生成」
                                val tid = currentTurnId.orEmpty()
                                userMsg.turnId = tid
                                assistant.turnId = tid
                            }
                            else -> ChatState.applyEvent(assistant, ev)
                        }
                        // 提案出现：本轮媒体 token 绑定到这张卡（确认时提交转正）
                        if (ev.event == "proposal") {
                            val seg = assistant.segs.lastOrNull() as? Seg.Tool
                            val aid = seg?.card?.action_id ?: 0
                            val intent = seg?.card?.intent.orEmpty()
                            // 同一条消息带的附件优先；没有就认领上一条消息里还没被认领的那批。
                            // 只对吃媒体的意图认领——删除、点赞这类卡不该把附件吞掉。
                            val media = turnMedia.ifEmpty { unclaimedMedia }
                            if (aid > 0 && media.isNotEmpty() && intent in MEDIA_INTENTS) cardMedia[aid] = media
                        }
                        scrollTick++
                    },
                )
            } catch (e: ChatState.ChatStreamException) {
                toast = e.message ?: "AI 出错了"
            } catch (e: Exception) {
                // 断连**不删消息**，保留已产生内容并标「已中断」。
                // 但「一条都没收到就断」不是「中断」而是失败——分开表述，否则用户
                // 看到的是「已中断」却没有任何内容，不知道该重试还是等。
                val produced = assistant.segs.any {
                    when (it) {
                        is Seg.Text -> it.text.isNotEmpty()
                        is Seg.Think -> it.text.isNotEmpty()
                        is Seg.Tool -> it.card != null
                    }
                }
                assistant.interrupted = true
                toast = if (produced) "连接中断，已保留已生成的内容" else "连接失败：${errText(e, "网络异常")}"
            } finally {
                scrollTick++
                streaming = false
            }
        }

    }

    // 重新生成：先删整轮（用户消息 + 回复 + 该轮模型上下文），再按原文重发。
    // 只对最后一条回复开放；该轮带附件或引用时不开放——那两样需要用户重新选，静默丢弃比不给按钮更糟。
    fun regenerate(turnId: String, text: String) {
        if (turnId.isEmpty() || streaming || text.isBlank()) return
        scope.launch {
            runCatching { ChatRepository.deleteTurn(turnId) }
                .onSuccess {
                    messages = messages.filterNot { it.turnId == turnId }
                    sendTurn(text, emptyList(), emptyList())
                }
                .onFailure { toast = errText(it, "重新生成失败") }
        }
    }

    // 提案裁决：点下去先乐观置态（防重复点），回执回来按服务端状态收口；
    // 失败/冲突（409 已裁决、410 过期）回滚成服务端真实状态，本地不猜。
    // 附件：**确认执行时才上传**——在此之前文件一直在设备上，取消/过期零残留。
    fun resolveCard(actionId: Long, decision: String) {
        val seg = messages.asReversed().asSequence()
            .flatMap { it.segs.asSequence() }
            .filterIsInstance<Seg.Tool>()
            .firstOrNull { it.card?.action_id == actionId }
        val before = seg?.card
        val media = cardMedia[actionId].orEmpty()
        seg?.card = before?.copy(status = if (decision == "approve") "executing" else "cancelled")
        scope.launch {
            var tokens = emptyList<String>()
            if (decision == "approve" && media.isNotEmpty()) {
                val got = mutableListOf<String>()
                var failed: String? = null
                media.forEachIndexed { i, m ->
                    if (failed != null) return@forEachIndexed
                    seg?.card = seg?.card?.copy(result = "上传中 ${i + 1}/${media.size}")
                    runCatching { MediaRepository.upload(appContext, m.uri) }
                        .onSuccess { got += it.media_token }
                        .onFailure { e -> failed = errText(e, "上传失败") ?: "上传失败" }
                }
                if (failed != null) {
                    // 不半发：一张传不上去就整张卡退回待确认，服务端一条媒体都没落库。
                    // 已经传上去的那几张退回临时区，不然要等 24h janitor。
                    if (got.isNotEmpty()) MediaRepository.discard(got)
                    seg?.card = before
                    toast = failed
                    return@launch
                }
                seg?.card = seg?.card?.copy(result = "")
                tokens = got
            }
            val outcome = runCatching { ChatRepository.resolveProposal(actionId, decision, tokens) }
            outcome
                .onSuccess { r ->
                    seg?.card = seg?.card?.copy(
                        status = r.status.ifEmpty { "executed" },
                        result = r.result ?: seg?.card?.result,
                    )
                    if (r.status == "failed") toast = r.error ?: "操作没能完成"
                    if (r.status != "failed") {
                        cardMedia.remove(actionId)
                        if (r.status == "executed") unclaimedMedia = emptyList() // 已被这张卡消费
                    }
                }
                .onFailure { e ->
                    val code = (e as? com.bookandmusic.lovelife.appnative.data.ApiError)?.status
                    seg?.card = when (code) {
                        410 -> before?.copy(status = "expired")
                        409 -> before
                        else -> before
                    }
                    toast = errText(e, "操作失败")
                }
            // 裁决没落库：本轮已上传的临时媒体主动撤销（成功执行时 token 已被服务端消费）
            if (outcome.isFailure && tokens.isNotEmpty()) MediaRepository.discard(tokens)
        }
    }

    // 媒体查看器：结果卡记录 → PostMediaUi（kind/thumb 兼容查看器三类）
    fun openMediaViewer(records: List<com.bookandmusic.lovelife.appnative.data.ChatUi.UiRecord>, index: Int) {
        val media = records.map { r ->
            com.bookandmusic.lovelife.appnative.data.PostMediaUi(
                id = r.id,
                kind = r.kind,
                thumbUrl = com.bookandmusic.lovelife.appnative.data.Channel.absolute(r.thumb_url),
                url = com.bookandmusic.lovelife.appnative.data.Channel.absolute(r.thumb_url),
            )
        }
        if (media.isNotEmpty()) viewer = media to index.coerceIn(0, media.size - 1)
    }

    // 消息条数或流式帧 → 滚到底（跟随输出）。
    // 流式期间用 scrollToItem（瞬时）：每帧都会重启这个副作用，动画永远走不完，反而抖。
    LaunchedEffect(messages.size, scrollTick) {
        if (messages.isEmpty()) return@LaunchedEffect
        if (streaming) listState.scrollToItem(messages.size - 1)
        else listState.animateScrollToItem(messages.size - 1)
    }

    // 根容器用 Box：错误提示要**浮**在输入区上方，而不是挤在 Column 末尾贴着屏幕底边
    Box(
        Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.surface)
            // 键盘打开时导航条 inset 已含在 IME inset 里：底部让 max(ime, navbar)，
            // 单用 imePadding 会在键盘收起时把导航条高度也漏掉（输入卡沉进手势区）。
            // consume 掉，避免子层（输入卡/列表）再算一次——那正是“输入卡被顶飞”的根因。
            .imePadding()
            .consumeWindowInsets(WindowInsets.ime)
            .padding(top = contentPadding.calculateTopPadding())
            // 整页跟手拖拽（仿 DeepSeek）：**整页任意位置**横向拖动都接管，不限起手区——
            // 右拉逐步展开、左拉收起。与列表垂直滚动共存：只有横向位移超过 slop 才由这里消费。
            .pointerInput(panelWidthPx) {
                var dragging = false
                detectHorizontalDragGestures(
                    onDragStart = { _ -> dragging = true },
                    onHorizontalDrag = { _, dx ->
                        if (!dragging) return@detectHorizontalDragGestures
                        val next = (drawerFraction + dx / panelWidthPx).coerceIn(0f, 1f)
                        if (next > 0f && drawerFraction == 0f) loadSessions()
                        drawerFraction = next
                    },
                    onDragEnd = {
                        if (dragging) settleDrawer(drawerFraction > 0.5f)
                        dragging = false
                    },
                    onDragCancel = {
                        if (dragging) settleDrawer(drawerFraction > 0.5f)
                        dragging = false
                    },
                )
            },
    ) {
        // 底层两种形态：会话列表 / 「选择对话」管理页（都静止，只有对话页右移）
        if (manageOpen) {
            ManageSessionsPage(
                sessions = sessions,
                onClose = { manageOpen = false },
                onDelete = { ids ->
                    manageOpen = false
                    deleteSessions(ids)
                },
            )
        } else {
            ChatSideDrawer(
                fraction = drawerFraction,
                sessions = sessions,
                currentId = sessionId,
                switching = switching,
                pendingLock = pendingLock,
                me = meState,
                myAvatar = myAvatar,
                onDismiss = { settleDrawer(false) },
                onSwitch = { id ->
                    settleDrawer(false)
                    switchSession(id)
                },
                onDelete = { deleteSession(it) },
                onPin = { togglePin(it) },
                onOpenSettings = {
                    settleDrawer(false)
                    onOpenSettings()
                },
                onManage = { manageOpen = true },
            )
        }
    // 抽屉推进时整页右移并轻微缩放：参考图是「页面被推开」而不是「蒙层上盖一层」
    Column(
        Modifier
            .fillMaxSize()
            .graphicsLayer {
                translationX = panelWidthPx * drawerFraction
                val k = 1f - 0.06f * drawerFraction
                scaleX = k
                scaleY = k
                transformOrigin = androidx.compose.ui.graphics.TransformOrigin(0f, 0.5f)
            }
            .clip(RoundedCornerShape(if (drawerFraction > 0.01f) 24.dp else 0.dp))
            .background(MaterialTheme.colorScheme.surface)
            .background(Color.Black.copy(alpha = 0.28f * drawerFraction))
            .then(
                if (drawerFraction > 0.01f) Modifier.clickable(
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                ) { settleDrawer(false); manageOpen = false } else Modifier
            ),
    ) {
        ChatTopBar(
            title = sessions.firstOrNull { it.id == sessionId }?.title?.ifEmpty { "新对话" } ?: "新对话",
            onNewSession = { newSession() },
            onOpenSessions = { loadSessions(); openDrawer() },
            // 流式期间会话/新建入口不可用（函数内 guard 也拦抽屉点击）
            enabled = !streaming,
        )

        // ===== 对话区 =====
        Box(Modifier.weight(1f)) {
            if (messages.isEmpty() && !streaming) {
                // 空会话（仿 DeepSeek 首页）：品牌图 + 一句问候，不放快捷 chips
                EmptyChat(welcome = boot?.welcome ?: "")
            } else {
// 跨轮引用：围栏里的 ref 是**会话级**的（模型上下文里一直带着旧句柄），
// 而 result_card part 只落在真正产出它的那一轮消息上——按会话合并卡片索引，
// 否则历史回显会把本来能渲染的卡显示成「引用的结果已失效」。
val sessionCards: Map<String, com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent> =
    buildMap { messages.forEach { m -> putAll(m.cards) } }
                LazyColumn(
                    state = listState,
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(
                        horizontal = 16.dp, vertical = 12.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    items(messages, key = { it.id }) { m ->
                        // 用户消息（新回合起点）前留白 20dp，其余 10dp（间距分档 20dp / 10dp）
                        val modifier = if (m.role == "user") Modifier.padding(top = 10.dp) else Modifier
                        Box(modifier) {
                        MessageItem(
                            m,
                            cards = sessionCards,
                            streaming = streaming,
                            onResolve = ::resolveCard,
                            onOpenMedia = ::openMediaViewer,
                            onMore = { sendMore() },
                            onToggleQuote = ::toggleQuote,
                            quotedIds = quotes.mapNotNull { it.record.id }.toSet(),
                            onRetryInterrupted = { text ->
                                // 编辑回填输入框（不自动发送）；本轮残局一并回收。
                                // 按 turnId 整轮删（用户 + 助手两条），不假设消息 id 相邻
                                val tid = m.turnId
                                messages = if (tid.isNotEmpty()) {
                                    messages.filterNot { it.turnId == tid }
                                } else {
                                    messages.filterNot { it.id == m.id }
                                }
                                input = text
                            },
                            // 能不能重放由 ChatState.regenerableTarget 一处判定（有单测守着）
                            onRegenerate = ChatState.regenerableTarget(messages, m)?.let { t ->
                                { regenerate(t.turnId, t.text) }
                            },
                        )
                        }
                    }
                }
            }
            if (!aiAvailable) {
                Text(
                    aiUnavailableReason,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.align(Alignment.Center).padding(horizontal = 32.dp),
                )
            }
        }

        // 引用条：勾选集在输入框上方，逐条可移除；随下一条消息提交
        if (quotes.isNotEmpty()) {
            Row(
                Modifier
                    .fillMaxWidth()
                    .background(MaterialTheme.colorScheme.surface)
                    .padding(horizontal = 16.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                quotes.take(3).forEach { q ->
                    Row(
                        Modifier
                            .clip(RoundedCornerShape(999.dp))
                            .background(MaterialTheme.colorScheme.primaryContainer)
                            .padding(horizontal = 10.dp, vertical = 4.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            q.record.text.ifEmpty { "#${q.record.id}" },
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurface,
                            maxLines = 1, overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.widthIn(max = 120.dp),
                        )
                        Text(
                            " ×",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier
                                .clickable { quotes = quotes.filterNot { it.record.id == q.record.id } }
                                .padding(horizontal = 2.dp),
                        )
                    }
                }
                if (quotes.size > 3) {
                    Text("+${quotes.size - 3}", style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }

        // ===== 输入区（chrome Bar 底栏） =====
        InputBar(
            text = input,
            onTextChange = { input = it },
            enabled = aiAvailable && !streaming,
            streaming = streaming,
            attachments = pendingMedia,
            onRemoveAttachment = { idx -> pendingMedia = pendingMedia.filterIndexed { i, _ -> i != idx } },
            onOpenMediaMenu = { showMediaMenu = true },
            onOpenCommands = { showCommands = true },
            onSend = {
                val text = input.trim()
                val turnMedia = pendingMedia
                if (text.isEmpty() && turnMedia.isEmpty()) return@InputBar
                input = ""
                pendingMedia = emptyList()
                if (turnMedia.isNotEmpty()) unclaimedMedia = turnMedia
                // 勾选按「结果集句柄 + 记录 id」上报（服务端按句柄查归属，裸 id/kind 都会被拒）；
                // label 是展示用摘要，服务端原样存回 quote part，刷新后气泡才能显示引用了什么
                val quotedRefs = quotes.map {
                    ChatRepository.QuoteRef(
                        result_id = it.ref,
                        ids = listOf(it.record.id),
                        label = it.record.text.ifBlank { "#${it.record.id}" }.take(40),
                    )
                }
                quotes = emptyList()
                sendTurn(text, turnMedia, quotedRefs)
            },
            onStop = {
                scope.launch {
                    currentTurnId?.let { runCatching { ChatRepository.abortTurn(it) } }
                    streaming = false
                }
            },
        )
        AppActionSheet(
            visible = showMediaMenu,
            items = listOf(
                AppMenuItem("照片", Icons.Outlined.Image) {
                    showMediaMenu = false
                    pickMedia.launch(
                        PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)
                    )
                },
                AppMenuItem("视频", Icons.Outlined.Videocam) {
                    showMediaMenu = false
                    pickMedia.launch(
                        PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.VideoOnly)
                    )
                },
                AppMenuItem("音频", Icons.Outlined.Audiotrack) {
                    showMediaMenu = false
                    pickAudio.launch(arrayOf("audio/*"))
                },
            ),
            onDismiss = { showMediaMenu = false },
            title = "添加附件",
        )
        if (showCommands) {
            CommandPanelSheet(
                onDismiss = { showCommands = false },
                onPick = { tpl ->
                    input = tpl
                    showCommands = false
                },
            )
        }
    }
        viewer?.let { (media, index) ->
            com.bookandmusic.lovelife.appnative.ui.MediaViewer(
                media = media,
                initialIndex = index,
                onDismiss = { viewer = null },
            )
        }

        // 错误提示：黑底白字胶囊，浮在输入区上方 8dp（不挡手、不贴边）
        AppToast(
            text = toast,
            onDismiss = { toast = null },
            modifier = Modifier.align(Alignment.BottomCenter),
            bottomPadding = contentPadding.calculateBottomPadding() + 56.dp,
        )
    }
}

/** 消息 media.type：优先用本机 mime（服务端只认 image/ video/ audio/ 前缀），退化按 kind 推 */
private fun attachMime(kind: String, mime: String): String {
    if (mime.startsWith("image/") || mime.startsWith("video/") || mime.startsWith("audio/")) return mime
    return when (kind) {
        "video" -> "video/attached"
        "audio" -> "audio/attached"
        else -> "image/attached"
    }
}

/**
 * 待随消息发出的媒体：**只有本机信息，没有 token**——文件留在设备上，
 * 用户点「确认执行」那一刻才上传换 token（避免没发出去的文件占服务端磁盘）。
 */
data class PendingMedia(
    val uri: android.net.Uri,
    /** 展示名（消息 media.name） */
    val name: String,
    /** 本机 mime，发送消息时作为 media.type 的来源 */
    val mime: String = "",
    val size: Long = 0L,
    /** photo / video / audio */
    val kind: String = "photo",
)

/** 会吃附件的提案意图：只有这几种才认领「未认领的附件」 */
private val MEDIA_INTENTS = setOf("propose_post", "propose_album", "propose_album_members")

// ---------- 顶栏 ----------

/** 当前窗口宽（dp）：抽屉 82%、气泡 78% 都按它算。用窗口容器尺寸而非 Configuration.screenWidthDp——
 *  分屏/多窗口下后者是「屏幕宽」，按它算出的抽屉会宽出窗口。 */
@Composable
private fun windowWidthDp(): Dp =
    with(androidx.compose.ui.platform.LocalDensity.current) {
        androidx.compose.ui.platform.LocalWindowInfo.current.containerSize.width.toDp()
    }

@Composable
/**
 * 顶栏（DeepSeek 同款）：左两行菜单键、居左会话标题（空会话 = 「新对话」）、右圈「+」新建。
 * [enabled] = false 用于流式回复期间：此时切换/新建会让在途流写回旧会话。
 */
private fun ChatTopBar(
    title: String,
    onNewSession: () -> Unit,
    onOpenSessions: () -> Unit,
    enabled: Boolean = true,
) {
    val colors = MaterialTheme.colorScheme
    val iconTint = if (enabled) colors.onSurface else colors.onSurface.copy(alpha = 0.38f)
    Row(
        Modifier.fillMaxWidth().height(52.dp).padding(horizontal = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier.size(40.dp).clip(CircleShape).clickable(
                enabled = enabled,
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
            ) { onOpenSessions() },
            contentAlignment = Alignment.Center,
        ) { MenuGlyph(tint = iconTint) }
        Text(
            title,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Medium,
            color = colors.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f).padding(start = 10.dp, end = 8.dp),
        )
        Box(
            Modifier.size(40.dp).clip(CircleShape).clickable(
                enabled = enabled,
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
            ) { onNewSession() },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                Icons.Outlined.AddCircleOutline,
                contentDescription = "新建会话",
                tint = iconTint,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

/** 两行菜单图标（上行长、下行短）：三线 Menu 是 Android 的默认语言，参考图不是这个 */
@Composable
private fun MenuGlyph(tint: Color = MaterialTheme.colorScheme.onSurface) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Box(Modifier.width(18.dp).height(2.dp).clip(RoundedCornerShape(2.dp)).background(tint))
        Box(Modifier.width(11.dp).height(2.dp).clip(RoundedCornerShape(2.dp)).background(tint))
    }
}

// ---------- 空会话 ----------

@Composable
private fun EmptyChat(welcome: String) {
    Column(
        Modifier.fillMaxSize().padding(horizontal = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        // 品牌图 + 一句问候（仿 DeepSeek 首页：logo 在问候语上方，居中，不铺三行提示）
        androidx.compose.foundation.Image(
            painter = androidx.compose.ui.res.painterResource(
                com.bookandmusic.lovelife.appnative.R.drawable.brand_icon
            ),
            contentDescription = "情侣生活记录",
            modifier = Modifier.size(72.dp).clip(brandTileShape(72.dp)),
        )
        Text(
            welcome,
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            modifier = Modifier.padding(top = 20.dp).widthIn(max = 340.dp),
        )
    }
}

// ---------- 消息 ----------

@Composable
private fun MessageItem(
    m: ChatState.RenderMessage,
    /** 会话级卡片索引（ref → 卡）：跨轮引用要靠它取到前几轮的结果卡 */
    cards: Map<String, com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent>,
    streaming: Boolean,
    onResolve: (Long, String) -> Unit,
    onOpenMedia: (List<com.bookandmusic.lovelife.appnative.data.ChatUi.UiRecord>, Int) -> Unit,
    onMore: () -> Unit,
    onToggleQuote: (UiQuoted) -> Unit,
    quotedIds: Set<Long>,
    onRetryInterrupted: (String) -> Unit,
    /** 非 null = 这条回复可以重新生成（只有尾条回复会给） */
    onRegenerate: (() -> Unit)?,
) {
    if (m.role == "user") {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Box(
                Modifier
                    .widthIn(max = windowWidthDp() * 0.78f)
                    .clip(RoundedCornerShape(20.dp))
                    .background(MaterialTheme.colorScheme.primaryContainer)
                    .padding(horizontal = 14.dp, vertical = 10.dp),
            ) {
                val userText = m.segs.filterIsInstance<Seg.Text>().joinToString("") { it.text }
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    // 引用条：发出去之后仍要看得见「引用了哪几条」（刷新后由 quote part 回填）
                    if (m.quotes.isNotEmpty()) {
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            m.quotes.take(3).forEach { label ->
                                Text(
                                    label,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onPrimaryContainer,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier
                                        .widthIn(max = 96.dp)
                                        .clip(RoundedCornerShape(999.dp))
                                        .background(MaterialTheme.colorScheme.surface.copy(alpha = 0.7f))
                                        .padding(horizontal = 8.dp, vertical = 3.dp),
                                )
                            }
                            if (m.quotes.size > 3) {
                                Text(
                                    "+${m.quotes.size - 3}",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onPrimaryContainer,
                                )
                            }
                        }
                    }
                    if (userText.isNotBlank()) {
                        Text(
                            userText,
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onPrimaryContainer,
                        )
                    }
                    // 附件指示：只带媒体没文字时气泡不能是空的（刷新后附件也得留痕）
                    if (m.mediaCount > 0) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(4.dp),
                        ) {
                            Icon(
                                Icons.Outlined.Image,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.75f),
                                modifier = Modifier.size(14.dp),
                            )
                            Text(
                                "附 ${m.mediaCount} 个媒体",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.75f),
                            )
                        }
                    }
                    if (userText.isBlank() && m.mediaCount == 0 && m.quotes.isEmpty()) {
                        Text(
                            "[图片]",
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onPrimaryContainer,
                        )
                    }
                }
            }
        }
    } else {
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            // ===== 折叠状态机 =====
            // concluded：**末段**是正文 = 正在出结论（不用 any{it is Text}：中间轮也可能带正文）。
            // 历史回显（!streaming）恒 concluded。
            val concluded = m.segs.lastOrNull() is Seg.Text || !streaming
            val toolSegs = m.segs.filterIsInstance<Seg.Tool>()
            val aggregate = concluded && toolSegs.size >= 2
            if (aggregate) AssistantRow { StepDigest(toolSegs) }
            val segs = mergeThinks(m.segs)
            segs.forEachIndexed { i, seg ->
                val isCurrent = i == segs.lastIndex && !concluded
                if (seg is Seg.Tool) {
                    val card = seg.card
                    val cardPending = card != null &&
                        ChatState.effectiveStatus(card.status, seg.cardExpireAt) == "pending"
                    val collapsed = !isCurrent && !cardPending && !seg.running
                    // 摘要态下无卡的步骤并入「已完成 N 步」，不逐行占位
                    if (aggregate && card == null) return@forEachIndexed
                    AssistantRow {
                        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            ToolBlock(seg, showStatusRow = !aggregate && !collapsed, collapsed = collapsed)
                            // 确认卡是「要你决策」的一等卡片：独立成块，不塞进灰底过程容器
                            card?.let { c ->
                                ConfirmCard(
                                    card = c,
                                    onResolve = { decision -> onResolve(c.action_id, decision) },
                                    expireAt = seg.cardExpireAt,
                                )
                            }
                        }
                    }
                } else {
                    AssistantRow {
                        AnswerBlock(
                            seg = seg,
                            cards = cards,
                            settled = !streaming,
                            onOpenMedia = onOpenMedia,
                            onMore = onMore,
                            onToggleQuote = onToggleQuote,
                            quotedIds = quotedIds,
                        )
                    }
                }
            }
            // 结果卡只由正文围栏渲染：没有围栏就没有卡（不按工具输出兜底画卡）
            // 动作行（DeepSeek 同款）：只对已出正文的回复出现，流式中不出现
            if (!streaming && m.segs.any { it is Seg.Text && it.text.isNotBlank() }) {
                AnswerActions(
                    text = m.segs.filterIsInstance<Seg.Text>().joinToString("") { it.text },
                    onRegenerate = onRegenerate,
                )
            }
            // 等待行：本轮还没产出任何内容（首帧前的干等最容易让人以为「卡住了」）
            if (streaming && m.segs.isEmpty()) {
                AssistantRow { WaitingLine() }
            }
            if (m.interrupted) {
                // 已中断：显示「已中断」+ 编辑重发（原文回填输入框，不自动发送）
                Row(Modifier.padding(top = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        "已中断",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Text(
                        "  编辑重发",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier
                            .clip(RoundedCornerShape(8.dp))
                            .clickable {
                                onRetryInterrupted(m.segs.filterIsInstance<Seg.Text>().joinToString("") { it.text })
                            }
                            .padding(horizontal = 4.dp, vertical = 2.dp),
                    )
                }
            }
        }
    }
}

/** 助手行：无头像——正文直接与左边界齐平，不留徽标槽 */
@Composable
private fun AssistantRow(content: @Composable () -> Unit) {
    Box(Modifier.fillMaxWidth()) { content() }
}

/**
 * 一轮的思考收成一行：模型可能思考多次（工具前后各一次），逐段渲染会看到
 * 「已思考（用时 3 秒）」重复两遍——参考图只有一行，用时相加、正文拼接。
 * 位置取**第一段**思考处：工具过程仍留在它后面。
 */
private fun mergeThinks(segs: List<Seg>): List<Seg> {
    val thinks = segs.filterIsInstance<Seg.Think>()
    if (thinks.size <= 1) return segs
    val merged = Seg.Think(
        text = thinks.map { it.text }.filter { it.isNotEmpty() }.joinToString("\n"),
        seconds = thinks.sumOf { it.seconds },
        done = thinks.last().done,
    )
    var placed = false
    return segs.mapNotNull { seg ->
        when {
            seg !is Seg.Think -> seg
            placed -> null
            else -> {
                placed = true
                merged
            }
        }
    }
}

/**
 * 回复动作行：重新生成 / 复制 / 分享（图标 18dp、灰、间距 22dp，与参考图同构）。
 * 重新生成只挂在最后一条回复上，由调用方决定是否给（null = 不给）。
 */
@Composable
private fun AnswerActions(text: String, onRegenerate: (() -> Unit)? = null) {
    val ctx = LocalContext.current
    val colors = MaterialTheme.colorScheme
    Row(
        Modifier.fillMaxWidth().padding(top = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(22.dp),
    ) {
        if (onRegenerate != null) {
            Icon(
                Icons.Outlined.Refresh,
                contentDescription = "重新生成",
                tint = colors.onSurfaceVariant,
                modifier = Modifier
                    .size(18.dp)
                    .clickable(
                        interactionSource = remember { MutableInteractionSource() },
                        indication = null,
                    ) { onRegenerate() },
            )
        }
        Icon(
            Icons.Outlined.ContentCopy,
            contentDescription = "复制",
            tint = colors.onSurfaceVariant,
            modifier = Modifier
                .size(18.dp)
                .clickable(
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                ) {
                    val cm = ctx.getSystemService(android.content.Context.CLIPBOARD_SERVICE)
                        as android.content.ClipboardManager
                    cm.setPrimaryClip(android.content.ClipData.newPlainText("回复", text))
                    android.widget.Toast.makeText(ctx, "已复制", android.widget.Toast.LENGTH_SHORT).show()
                },
        )
        Icon(
            Icons.Outlined.IosShare,
            contentDescription = "分享",
            tint = colors.onSurfaceVariant,
            modifier = Modifier
                .size(18.dp)
                .clickable(
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                ) {
                    val send = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
                        type = "text/plain"
                        putExtra(android.content.Intent.EXTRA_TEXT, text)
                    }
                    ctx.startActivity(android.content.Intent.createChooser(send, "分享"))
                },
        )
    }
}

/** 首帧前的等待行：不转圈（转圈像加载失败），一行灰字 + 呼吸点 */
@Composable
private fun WaitingLine() {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(vertical = 4.dp)) {
        Box(
            Modifier
                .size(6.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(MaterialTheme.colorScheme.primary),
        )
        Text(
            "  正在连接…",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/**
 * 助手正文（DeepSeek 同款）：**无壳**纯文本，围栏卡片就地插入正文流。
 * 卡片自带容器，正文不包白卡——两层边框是视觉噪音。
 */
@Composable
private fun AnswerBlock(
    seg: Seg,
    cards: Map<String, com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent>,
    /** 本轮已结束：false 时引用不到的卡算「还在路上」，不报错也不占位 */
    settled: Boolean,
    onOpenMedia: (List<com.bookandmusic.lovelife.appnative.data.ChatUi.UiRecord>, Int) -> Unit,
    onMore: () -> Unit,
    onToggleQuote: (UiQuoted) -> Unit,
    quotedIds: Set<Long>,
) {
    when (seg) {
        is Seg.Think -> ThinkBlock(seg)
        is Seg.Text -> Column(
            Modifier.fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            val blocks = UiFence.parse(seg.text, cards, complete = seg.done, settled = settled)
            blocks.forEach { b ->
                when (b) {
                    is UiFence.Block.Text -> if (b.text.isNotBlank()) {
                        // 可拖选：整段复制之外还要能挑一段（DeepSeek 同款手感）
                        androidx.compose.foundation.text.selection.SelectionContainer {
                            Text(
                                markdownish(
                                    b.text,
                                    cursorColor = if (seg.done) null else MaterialTheme.colorScheme.primary,
                                ),
                                style = MaterialTheme.typography.bodyLarge.copy(
                                    lineHeight = MaterialTheme.typography.bodyLarge.fontSize * 1.6f,
                                ),
                                color = MaterialTheme.colorScheme.onSurface,
                            )
                        }
                    }
                    is UiFence.Block.Card -> ChartBlock(b, onOpenMedia, onMore, onToggleQuote, quotedIds)
                    is UiFence.Block.Pending -> Unit
                    is UiFence.Block.Failed -> Text(
                        // reason 本身就是一句人话（含它自己的括号说明），不套括号
                        b.reason,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        is Seg.Tool -> Unit
    }
}

/** 围栏卡片块：标题进卡头，note 作为卡下一行灰字 */
@Composable
private fun ChartBlock(
    b: UiFence.Block.Card,
    onOpenMedia: (List<com.bookandmusic.lovelife.appnative.data.ChatUi.UiRecord>, Int) -> Unit,
    onMore: () -> Unit,
    onToggleQuote: (UiQuoted) -> Unit,
    quotedIds: Set<Long>,
) {
    Column(Modifier.fillMaxWidth()) {
        val data = b.data.copy(title = b.event.title.ifBlank { b.data.title })
        DisplayCard(
            view = b.event.view,
            data = data,
            only = b.event.only,
            limit = b.event.limit,
            onMediaClick = { index, records -> onOpenMedia(records, index) },
            onMore = { onMore() },
            onToggleQuote = if (data.selectable) onToggleQuote else null,
            quotedIds = quotedIds,
        )
        if (b.event.note.isNotBlank()) {
            Text(
                b.event.note,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}

/**
 * 思考块：无壳竖线行。默认展开仅当「还在思考」（未被正文接管）；
 * 折叠后「思考 · 已思考 N 秒 ›」可点开临时看，不自动展开。
 */
@Composable
private fun ThinkBlock(seg: Seg.Think) {
    var expanded by remember(seg.done) { mutableStateOf(!seg.done) }
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .clip(RoundedCornerShape(8.dp))
                .clickable(enabled = seg.done) { expanded = !expanded }
                .padding(vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                if (seg.done) "已思考（用时 ${seg.seconds} 秒）" else "思考中…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            // 可点开的可见性：没有 chevron 时，用户不知道这行能展开
            if (seg.done && seg.text.isNotEmpty()) {
                Icon(
                    imageVector = if (expanded) Icons.Outlined.KeyboardArrowDown else Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                    contentDescription = if (expanded) "收起思考" else "展开思考",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 4.dp).size(14.dp),
                )
            }
        }
        if (expanded && seg.text.isNotEmpty()) {
            // DeepSeek 同款：左竖线引用块 + 灰字，封顶 180dp 内部滚动
            Row(Modifier.padding(start = 6.dp, bottom = 4.dp)) {
                Box(
                    Modifier
                        .width(3.dp)
                        .heightIn(min = 40.dp)
                        .fillMaxHeight()
                        .clip(RoundedCornerShape(2.dp))
                        .background(MaterialTheme.colorScheme.outlineVariant),
                )
                Text(
                    seg.text,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier
                        .padding(start = 10.dp, top = 2.dp)
                        .heightIn(max = 180.dp)
                        .verticalScroll(rememberScrollState()),
                )
            }
        }
    }
}

/**
 * 「已完成 N 步」（一轮结束后聚合）：多次工具调用收成一条可展开摘要，
 * 展开看到的是每步人话状态（不是技术细节——技术审计归后台）。
 * 无壳、与思考块同层，只用 11 号灰字，不抢对话正文的层级。
 */
@Composable
private fun StepDigest(tools: List<Seg.Tool>) {
    var expanded by remember(tools.size) { mutableStateOf(false) }
    val colors = MaterialTheme.colorScheme
    Column(Modifier.fillMaxWidth().padding(start = 12.dp)) {
        Row(
            Modifier
                .clip(RoundedCornerShape(8.dp))
                .clickable { expanded = !expanded }
                .padding(vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "✓ 已完成 ${tools.size} 步",
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                if (expanded) "   收起" else "   展开",
                style = MaterialTheme.typography.bodySmall,
                color = colors.primary,
            )
        }
        if (expanded) {
            tools.forEach { t ->
                Text(
                    "· ${t.label}${if (t.resultText.isNotEmpty()) " → ${t.resultText}" else ""}",
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier.padding(start = 8.dp, top = 2.dp),
                )
            }
        }
    }
}

/**
 * 工具块（过程层）：只有一行人话状态，**不出卡片**。
 * - 当前环节：灰底容器 + 转圈/✓ + 「动作 → 结果」
 * - 非当前环节：无壳一行灰字
 * 结果卡由正文围栏渲染、确认卡由 MessageItem 单独渲染——过程层只管「做了什么」。
 */
@Composable
private fun ToolBlock(
    seg: Seg.Tool,
    /** 摘要态下状态行已被「已完成 N 步」替代 */
    showStatusRow: Boolean = true,
    collapsed: Boolean = false,
) {
    val colors = MaterialTheme.colorScheme
    val text = if (seg.resultText.isNotEmpty()) "${seg.label} → ${seg.resultText}" else seg.label
    if (collapsed) {
        if (!showStatusRow) return
        Row(
            Modifier.fillMaxWidth().padding(start = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("✓ ", style = MaterialTheme.typography.bodySmall, color = colors.primary)
            Text(
                text,
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        return
    }
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(colors.surfaceContainerHigh)
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
        if (showStatusRow) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                if (seg.running) {
                    CircularProgressIndicator(
                        strokeWidth = 1.8.dp,
                        modifier = Modifier.padding(end = 8.dp).size(14.dp),
                    )
                } else {
                    Text("✓ ", style = MaterialTheme.typography.bodySmall, color = colors.primary)
                }
                Text(
                    if (seg.running) seg.label else text,
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

// ---------- 输入区 ----------

@Composable
private fun InputBar(
    text: String,
    onTextChange: (String) -> Unit,
    enabled: Boolean,
    streaming: Boolean,
    onSend: () -> Unit,
    onStop: () -> Unit,
    onOpenMediaMenu: () -> Unit,
    onOpenCommands: () -> Unit,
    attachments: List<PendingMedia> = emptyList(),
    onRemoveAttachment: (Int) -> Unit = {},
) {
    val colors = MaterialTheme.colorScheme
    val appColors = LocalAppColors.current
    val canSend = enabled && (text.isNotBlank() || attachments.isNotEmpty())
    // 底部输入区（DeepSeek 同款）：一块大圆角白卡，上排输入、下排「功能胶囊 + 圆键」。
    // 发送键只在有内容时出现——空输入时右侧只有一个「+」，界面不摆用不上的按钮。
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 10.dp, vertical = 8.dp),
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .shadow(12.dp, RoundedCornerShape(20.dp), clip = false)
                .clip(RoundedCornerShape(20.dp))
                .background(colors.surface)
                .border(0.5.dp, appColors.cardEdge, RoundedCornerShape(20.dp))
                .padding(horizontal = 14.dp, vertical = 12.dp)
                .then(if (enabled) Modifier else Modifier.alpha(0.55f)),
        ) {
            // 已选媒体：缩略图一行（可逐个移除）。**不发之前不上传**，所以没有上传占位
            if (attachments.isNotEmpty()) {
                Row(
                    Modifier.fillMaxWidth().padding(bottom = 8.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    attachments.forEachIndexed { i, m ->
                        Box(Modifier.size(56.dp)) {
                            AsyncImage(
                                model = m.uri,
                                contentDescription = null,
                                contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                                modifier = Modifier.fillMaxSize().clip(RoundedCornerShape(12.dp))
                                    .background(colors.surfaceContainerHigh),
                            )
                            Text(
                                "×",
                                style = MaterialTheme.typography.labelLarge,
                                color = Color.White,
                                modifier = Modifier
                                    .align(Alignment.TopEnd)
                                    .padding(2.dp)
                                    .clip(RoundedCornerShape(999.dp))
                                    .background(Color.Black.copy(alpha = 0.45f))
                                    .clickable { onRemoveAttachment(i) }
                                    .padding(horizontal = 5.dp),
                            )
                        }
                    }
                }
            }
            // 上排：输入区（占位与正文同档）
            Box(Modifier.fillMaxWidth().heightIn(min = 44.dp), contentAlignment = Alignment.TopStart) {
                if (text.isEmpty()) {
                    Text(
                        "发消息…",
                        style = MaterialTheme.typography.bodyLarge,
                        color = colors.onSurfaceVariant,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
                BasicTextField(
                    value = text,
                    onValueChange = onTextChange,
                    enabled = enabled,
                    textStyle = MaterialTheme.typography.bodyLarge.copy(
                        color = colors.onSurface,
                        lineHeight = MaterialTheme.typography.bodyLarge.fontSize * 1.4f,
                    ),
                    cursorBrush = androidx.compose.ui.graphics.SolidColor(appColors.tint),
                    modifier = Modifier.fillMaxWidth(),
                    maxLines = 5,
                )
            }
            // 下排：左「照片」胶囊 + 右「+」圆键 / 发送圆键
            Row(
                Modifier.fillMaxWidth().padding(top = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                InputPill(
                    label = "附件",
                    icon = Icons.Outlined.AttachFile,
                    enabled = enabled,
                    onClick = onOpenMediaMenu,
                )
                Spacer(Modifier.weight(1f))
                CircleKey(
                    icon = Icons.Outlined.Add,
                    contentDescription = "更多",
                    enabled = enabled,
                    onClick = onOpenCommands,
                )
                Spacer(Modifier.width(8.dp))
                when {
                    streaming -> CircleKey(
                        icon = null,
                        contentDescription = "停止生成",
                        enabled = true,
                        onClick = onStop,
                        filled = colors.error,
                    ) {
                        Box(
                            Modifier.size(10.dp).clip(RoundedCornerShape(2.dp))
                                .background(colors.onError),
                        )
                    }
                    canSend -> CircleKey(
                        icon = Icons.Outlined.ArrowUpward,
                        contentDescription = "发送",
                        enabled = true,
                        onClick = onSend,
                        filled = colors.primary,
                        tint = appColors.onSolid,
                    )
                    else -> CircleKey(
                        icon = Icons.Outlined.ArrowUpward,
                        contentDescription = "发送",
                        enabled = canSend,
                        onClick = onSend,
                        filled = if (canSend) colors.primary else colors.surfaceContainerHigh,
                        tint = if (canSend) appColors.onSolid else colors.onSurfaceVariant,
                    )
                }
            }
        }
        // 免责声明：输入卡下方居中一行灰字
        Text(
            "回答由 AI 生成，仅供参考",
            style = MaterialTheme.typography.labelMedium,
            color = colors.onSurfaceVariant,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
        )
    }
}

/** 输入区功能胶囊（主色 8% 底 + 主色图标文字，仿 DeepSeek 的「深度思考」档） */
@Composable
private fun InputPill(
    label: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Row(
        Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(colors.primary.copy(alpha = 0.08f))
            .border(1.dp, colors.primary.copy(alpha = 0.25f), RoundedCornerShape(999.dp))
            .clickable(enabled = enabled) { onClick() }
            .padding(horizontal = 12.dp, vertical = 7.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = colors.primary, modifier = Modifier.size(15.dp))
        Text(
            label,
            style = MaterialTheme.typography.labelLarge,
            color = colors.primary,
            modifier = Modifier.padding(start = 5.dp),
        )
    }
}

/** 输入区圆键：描边（+）或实心（发送/停止） */
@Composable
private fun CircleKey(
    icon: androidx.compose.ui.graphics.vector.ImageVector?,
    contentDescription: String,
    enabled: Boolean,
    onClick: () -> Unit,
    filled: Color? = null,
    tint: Color = MaterialTheme.colorScheme.onSurfaceVariant,
    content: (@Composable () -> Unit)? = null,
) {
    val colors = MaterialTheme.colorScheme
    Box(
        Modifier
            .size(36.dp)
            .clip(CircleShape)
            .then(
                if (filled != null) Modifier.background(filled)
                else Modifier.border(1.dp, colors.onSurfaceVariant.copy(alpha = 0.7f), CircleShape)
            )
            .clickable(enabled = enabled) { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        if (content != null) {
            content()
        } else if (icon != null) {
            Icon(icon, contentDescription = contentDescription, tint = tint, modifier = Modifier.size(18.dp))
        }
    }
}

/**
 * 轻量 Markdown（标题 / 列表 / 行内代码 / 围栏代码块 / 链接 / 引用行 / 粗体）。
 * 不引第三方 md 库：渲染目标是 Compose 的 AnnotatedString，第三方库要么只出 HTML，
 * 要么带自己的主题色（越界到 designsystem 之外）。代码块只做等宽，不做语法高亮。
 */
/** 列表悬挂缩进：续行对齐到项目符号之后（26dp ≈「  •  」的宽度） */
private val BULLET_INDENT = 26.dp

@Composable
internal fun markdownish(src: String, cursorColor: Color? = null): AnnotatedString {
    // 引用竖线的颜色必须取 token：写死色值违反色板约束（颜色只经 MaterialTheme），
    // 也会在深色模式下变成一条看不懂的灰。
    val quoteColor = MaterialTheme.colorScheme.onSurfaceVariant
    // dp → sp 走密度换算：:app 禁止出现 .sp 字面量（StyleGuardTest）
    val restIndent = with(LocalDensity.current) { BULLET_INDENT.toSp() }
    return markdownOf(src, quoteColor, cursorColor, restIndent)
}

/** 纯逻辑主体（可单测）：引用色由调用方注入 */
internal fun markdownOf(
    src: String,
    quoteColor: Color,
    cursorColor: Color? = null,
    restIndent: TextUnit = TextUnit.Unspecified,
): AnnotatedString = buildAnnotatedString {
    val lines = src.split('\n')
    var inFence = false
    for ((li, raw) in lines.withIndex()) {
        val line = raw

        // 围栏代码块：``` 开 / 关；中间原样保留（不做任何行内解析）
        if (line.trimStart().startsWith("```")) {
            inFence = !inFence
            if (li != lines.lastIndex) append('\n')
            continue
        }
        if (inFence) {
            withStyle(SpanStyle(fontFamily = FontFamily.Monospace)) { append(line) }
            if (li != lines.lastIndex) append('\n')
            continue
        }

        val heading = Regex("^#{1,4}\\s+(.*)").matchEntire(line)
        val bullet = Regex("^\\s*[-*+]\\s+(.*)").matchEntire(line)
        val ordered = Regex("^\\s*(\\d+)\\.\\s+(.*)").matchEntire(line)
        val quote = Regex("^\\s*>\\s?(.*)").matchEntire(line)
        when {
            heading != null -> {
                withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(heading.groupValues[1]) }
            }
            bullet != null -> {
                // 列表符号换成圆点缩进：原文的 -/* 在聊天里读起来像破折号。
                // 续行必须缩进到符号之后，否则折行会贴回圆点左缘（长条目一眼看出是排版错）
                withStyle(ParagraphStyle(textIndent = TextIndent(restLine = restIndent))) {
                    append("  •  ")
                    appendInline(bullet.groupValues[1])
                }
            }
            ordered != null -> {
                withStyle(ParagraphStyle(textIndent = TextIndent(restLine = restIndent))) {
                    append("  ${ordered.groupValues[1]}. ")
                    appendInline(ordered.groupValues[2])
                }
            }
            quote != null -> {
                withStyle(SpanStyle(color = quoteColor)) { append("│ ") }
                appendInline(quote.groupValues[1])
            }
            else -> appendInline(line)
        }
        if (li != lines.lastIndex) append('\n')
    }
    // 流式光标：放在整段末尾，随每个 delta 重组而「跟着字走」
    if (cursorColor != null) withStyle(SpanStyle(color = cursorColor)) { append(" ▍") }
}

/** 行内语法：`代码` / [文字](链接) / **粗体**（顺序固定：先代码再链接再粗体，避免互相吞标记） */
private fun AnnotatedString.Builder.appendInline(src: String) {
    var i = 0
    while (i < src.length) {
        val ch = src[i]
        when {
            ch == '`' -> {
                val end = src.indexOf('`', i + 1)
                if (end < 0) { append(ch); i++ } else {
                    withStyle(SpanStyle(fontFamily = FontFamily.Monospace)) {
                        append(src.substring(i + 1, end))
                    }
                    i = end + 1
                }
            }
            ch == '[' -> {
                val closeBracket = src.indexOf(']', i + 1)
                val openParen = if (closeBracket >= 0) src.indexOf('(', closeBracket + 1) else -1
                val closeParen = if (openParen >= 0) src.indexOf(')', openParen + 1) else -1
                if (closeBracket < 0 || openParen != closeBracket + 1 || closeParen < 0) {
                    append(ch); i++
                } else {
                    // 只留文字、加下划线样式：聊天正文里点击跳外链是模型不该有的能力（也是越界面）
                    withStyle(SpanStyle(textDecoration = TextDecoration.Underline)) {
                        append(src.substring(i + 1, closeBracket))
                    }
                    i = closeParen + 1
                }
            }
            src.startsWith("**", i) -> {
                val end = src.indexOf("**", i + 2)
                if (end < 0) { append(ch); i++ } else {
                    withStyle(SpanStyle(fontWeight = FontWeight.Bold)) {
                        append(src.substring(i + 2, end))
                    }
                    i = end + 2
                }
            }
            else -> { append(ch); i++ }
        }
    }
}


// ---------- 会话抽屉 ----------

/** 时间分组标签：今天 / 7天内 / 更早（updated_at 是 UTC，先换算成本地日期再比） */
internal fun groupLabelOf(updatedAt: String, today: java.time.LocalDate = java.time.LocalDate.now()): String {
    val d = sessionDate(updatedAt) ?: return "更早"
    val days = java.time.temporal.ChronoUnit.DAYS.between(d, today)
    if (days == 0L) return "今天"
    if (days in 1..7) return "7天内"
    return monthLabelOf(d)
}

/** 会话时间换算成本地日期；解析失败返回 null（这类会话归「更早」） */
private fun sessionDate(updatedAt: String): java.time.LocalDate? =
    com.bookandmusic.lovelife.appnative.util.TimeFmt.parse(updatedAt)?.toLocalDate()

/** 月份组标签：展示用「2026年9月」。排序不能拿它比大小，否则「9月」排在「10月」前面 */
private fun monthLabelOf(d: java.time.LocalDate): String = "${d.year}年${d.monthValue}月"

/**
 * 会话分组（抽屉与管理页共用）：置顶（有才出）→ 今天 → 7天内 → 最近两个月份 → 更早。
 * 月份组最多两个，其余**显式**并入「更早」——只把前两个月份当标签、再按标签精确匹配，
 * 会让其余月份的会话不属于任何一组，在抽屉与管理页直接消失（打不开也删不掉）。
 * 月份排序用日期键，不用展示串。
 */
internal fun groupedSessions(
    sessions: List<ChatRepository.SessionInfo>,
): List<Pair<String, List<ChatRepository.SessionInfo>>> {
    val pinned = sessions.filter { it.pinned }
    val rest = sessions.filter { !it.pinned }
    // 只有「今天 / 7天内」之外的会话才贡献月份组：否则本月会白占一个月份名额
    val keptMonths = rest.filter {
        val label = groupLabelOf(it.updated_at)
        label != "今天" && label != "7天内"
    }.mapNotNull { sessionDate(it.updated_at) }
        .distinct()
        .sortedDescending()
        .take(2)
    val labels = buildList {
        if (pinned.isNotEmpty()) add("置顶")
        add("今天")
        add("7天内")
        addAll(keptMonths.map { monthLabelOf(it) })
        add("更早")
    }.distinct()

    fun groupOf(s: ChatRepository.SessionInfo): String {
        val label = groupLabelOf(s.updated_at)
        if (label == "今天" || label == "7天内") return label
        val month = sessionDate(s.updated_at)
        return if (month != null && month in keptMonths) monthLabelOf(month) else "更早"
    }

    return labels.map { g ->
        g to if (g == "置顶") pinned else rest.filter { groupOf(it) == g }
    }
}

/**
 * 会话抽屉（DeepSeek 同款）：跟手位移 [fraction] 0f 收起 / 1f 展开。
 * 顶部搜索胶囊 + 按时间分组的会话列表 + 底部「我」。列表项右侧「…」是唯一的操作入口。
 */
@Composable
private fun ChatSideDrawer(
    fraction: Float,
    sessions: List<ChatRepository.SessionInfo>,
    currentId: Long,
    switching: Boolean,
    pendingLock: Boolean,
    me: com.bookandmusic.lovelife.appnative.data.UserInfo?,
    /** 我的真实头像 URL（/space 成员带签名）：有就画图，没有退回首字母 */
    myAvatar: String,
    onDismiss: () -> Unit,
    onSwitch: (Long) -> Unit,
    onDelete: (Long) -> Unit,
    onPin: (Long) -> Unit,
    onOpenSettings: () -> Unit,
    onManage: () -> Unit,
) {
    if (fraction <= 0f) return
    val colors = MaterialTheme.colorScheme
    var query by remember { mutableStateOf("") }
    var menuFor by remember { mutableStateOf<ChatRepository.SessionInfo?>(null) }
    // 会话列表是**底层静态页**：只有对话页右移把它「让」出来，列表自身不动（参考图行为）
    Box(Modifier.fillMaxSize()) {
        Column(
            Modifier
                .width(windowWidthDp() * 0.82f)
                .fillMaxHeight()
                .clip(RoundedCornerShape(topEnd = 20.dp, bottomEnd = 20.dp))
                .background(colors.surface)
                .navigationBarsPadding(),
        ) {
            // 搜索胶囊（参考图没有标题行，抽屉第一眼就是搜索）
            Row(
                Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 16.dp, top = 14.dp)
                    .height(44.dp)
                    .clip(RoundedCornerShape(999.dp))
                    .background(colors.surfaceContainerHigh)
                    .padding(horizontal = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    Icons.Outlined.Search,
                    contentDescription = null,
                    tint = colors.onSurfaceVariant,
                    modifier = Modifier.size(18.dp),
                )
                Box(Modifier.weight(1f).padding(start = 8.dp)) {
                    if (query.isEmpty()) {
                        Text(
                            "搜索会话",
                            style = MaterialTheme.typography.bodyLarge,
                            color = colors.onSurfaceVariant,
                        )
                    }
                    BasicTextField(
                        value = query,
                        onValueChange = { query = it },
                        textStyle = MaterialTheme.typography.bodyLarge.copy(color = colors.onSurface),
                        cursorBrush = androidx.compose.ui.graphics.SolidColor(colors.primary),
                        modifier = Modifier.fillMaxWidth(),
                        maxLines = 1,
                    )
                }
            }
            // 管理入口：右上角一枚「勾选管理」图标 → 选择对话页
            Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp)) {
                Spacer(Modifier.weight(1f))
                Box(
                    Modifier.size(32.dp).clip(CircleShape).clickable(
                        interactionSource = remember { MutableInteractionSource() },
                        indication = null,
                    ) { onManage() },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.Outlined.Checklist,
                        contentDescription = "管理对话",
                        tint = colors.onSurfaceVariant,
                        modifier = Modifier.size(20.dp),
                    )
                }
            }
            val filtered = sessions.filter { query.isBlank() || it.title.contains(query, ignoreCase = true) }
            Column(
                Modifier.weight(1f).verticalScroll(rememberScrollState())
                    .padding(horizontal = 12.dp),
            ) {
                groupedSessions(filtered).forEach { (g, rows) ->
                    if (rows.isNotEmpty()) {
                        Text(
                            g,
                            style = MaterialTheme.typography.labelMedium,
                            color = colors.onSurfaceVariant,
                            modifier = Modifier.padding(start = 8.dp, top = 18.dp, bottom = 6.dp),
                        )
                        rows.forEach { s ->
                            SessionRow(
                                s = s,
                                current = s.id == currentId,
                                blocked = pendingLock && s.id == currentId,
                                switching = switching,
                                onSwitch = onSwitch,
                                onMore = { menuFor = s },
                            )
                        }
                    }
                }
                if (filtered.isEmpty()) {
                    Text(
                        if (query.isBlank()) "还没有会话" else "没有匹配的对话",
                        style = MaterialTheme.typography.bodyMedium,
                        color = colors.onSurfaceVariant,
                        modifier = Modifier.padding(start = 8.dp, top = 16.dp),
                    )
                }
            }
            // 底部：我（点开设置）
            Row(
                Modifier
                    .fillMaxWidth()
                    .clickable { onOpenSettings() }
                    .padding(horizontal = 18.dp, vertical = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                AppAvatar(
                    name = me?.nickname ?: "我",
                    role = me?.role,
                    size = 34.dp,
                    image = if (myAvatar.isNotBlank()) {
                        {
                            AsyncImage(
                                model = myAvatar,
                                contentDescription = null,
                                contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                                modifier = Modifier.fillMaxSize(),
                            )
                        }
                    } else {
                        null
                    },
                )
                Text(
                    me?.nickname ?: "我",
                    style = MaterialTheme.typography.bodyLarge,
                    color = colors.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f).padding(start = 10.dp),
                )
                Icon(
                    Icons.Outlined.MoreHoriz,
                    contentDescription = "设置",
                    tint = colors.onSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
        // 会话操作单：删除（危险色）
        AppActionSheet(
            visible = menuFor != null,
            items = listOf(
                AppMenuItem(
                    label = if (menuFor?.pinned == true) "取消置顶" else "置顶会话",
                    icon = Icons.Outlined.PushPin,
                    onClick = {
                        menuFor?.let { onPin(it.id) }
                        menuFor = null
                    },
                ),
                AppMenuItem(
                    label = "删除会话",
                    icon = Icons.Outlined.DeleteOutline,
                    danger = true,
                    onClick = {
                        menuFor?.let { onDelete(it.id) }
                        menuFor = null
                    },
                ),
            ),
            onDismiss = { menuFor = null },
            title = menuFor?.title?.ifEmpty { "新对话" },
        )
    }
}

/**
 * 「选择对话」管理页（参考图 06）：多选会话 → 批量删除。
 * 与抽屉同层（都在对话页之下）：打开时对话页仍被推到右侧，只是左下的内容换成管理页。
 */
@Composable
private fun ManageSessionsPage(
    sessions: List<ChatRepository.SessionInfo>,
    onClose: () -> Unit,
    onDelete: (List<Long>) -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    var picked by remember { mutableStateOf<Set<Long>>(emptySet()) }
    Column(
        Modifier
            .fillMaxHeight()
            .width(windowWidthDp() * 0.82f)
            .background(colors.surface),
    ) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 16.dp, top = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "选择对话",
                style = MaterialTheme.typography.headlineSmall,
                color = colors.onSurface,
                modifier = Modifier.weight(1f),
            )
            Box(
                Modifier
                    .size(36.dp)
                    .clip(CircleShape)
                    .background(colors.surfaceContainerHigh)
                    .clickable(
                        interactionSource = remember { MutableInteractionSource() },
                        indication = null,
                    ) { onClose() },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.Outlined.Close,
                    contentDescription = "关闭",
                    tint = colors.onSurface,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
        Column(
            Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = 12.dp),
        ) {
            groupedSessions(sessions).forEach { (g, rows) ->
                if (rows.isNotEmpty()) {
                    Text(
                        g,
                        style = MaterialTheme.typography.labelMedium,
                        color = colors.onSurfaceVariant,
                        modifier = Modifier.padding(start = 8.dp, top = 18.dp, bottom = 6.dp),
                    )
                    rows.forEach { s ->
                        val on = s.id in picked
                        Row(
                            Modifier
                                .fillMaxWidth()
                                .clip(RoundedCornerShape(12.dp))
                                .clickable { picked = if (on) picked - s.id else picked + s.id }
                                .padding(horizontal = 8.dp, vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Box(
                                Modifier
                                    .size(22.dp)
                                    .clip(CircleShape)
                                    .background(if (on) colors.primary else Color.Transparent)
                                    .border(
                                        1.5.dp,
                                        if (on) colors.primary else colors.outlineVariant,
                                        CircleShape,
                                    ),
                                contentAlignment = Alignment.Center,
                            ) {
                                if (on) {
                                    Icon(
                                        Icons.Outlined.Check,
                                        contentDescription = null,
                                        tint = colors.onPrimary,
                                        modifier = Modifier.size(14.dp),
                                    )
                                }
                            }
                            Text(
                                s.title.ifEmpty { "新对话" },
                                style = MaterialTheme.typography.bodyLarge,
                                color = colors.onSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f).padding(start = 12.dp),
                            )
                        }
                    }
                }
            }
        }
        // 底部操作条：未选时整条置灰（与参考图一致），选中后可删
        Box(Modifier.fillMaxWidth().height(0.5.dp).background(colors.outlineVariant.copy(alpha = 0.6f)))
        Row(
            Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                if (picked.isEmpty()) "未选择会话" else "已选 ${picked.size} 个",
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            Text(
                "删除",
                style = MaterialTheme.typography.bodyLarge,
                color = if (picked.isEmpty()) colors.onSurfaceVariant.copy(alpha = 0.4f) else colors.error,
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .clickable(enabled = picked.isNotEmpty()) { onDelete(picked.toList()) }
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            )
        }
    }
}

@Composable
private fun SessionRow(
    s: ChatRepository.SessionInfo,
    current: Boolean,
    blocked: Boolean,
    switching: Boolean,
    onSwitch: (Long) -> Unit,
    onMore: () -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Row(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 1.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(if (current) colors.primary.copy(alpha = 0.08f) else Color.Transparent)
            .clickable(enabled = !switching && !blocked) { onSwitch(s.id) }
            .padding(start = 12.dp, end = 6.dp, top = 12.dp, bottom = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (s.pinned) {
            Icon(
                Icons.Outlined.PushPin,
                contentDescription = "已置顶",
                tint = if (current) colors.primary else colors.onSurfaceVariant,
                modifier = Modifier.padding(end = 6.dp).size(14.dp),
            )
        }
        Text(
            s.title.ifEmpty { "新对话" },
            style = MaterialTheme.typography.bodyLarge,
            color = if (current) colors.primary else colors.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Box(
            Modifier
                .size(28.dp)
                .clip(CircleShape)
                .clickable(enabled = !switching && !blocked) { onMore() },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                Icons.Outlined.MoreHoriz,
                contentDescription = "会话操作",
                tint = if (current) colors.primary else colors.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

// ---------- 「+」面板 ----------

/**
 * 「+」面板：只放五条快捷提问——点一下**填进输入框**、不直接发送。
 * 媒体入口不在这里：输入区左侧的「附件」胶囊负责照片 / 视频 / 音频。
 */
@Composable
private fun CommandPanelSheet(
    onDismiss: () -> Unit,
    onPick: (template: String) -> Unit,
) {
    val prompts = listOf(
        "回顾去年今天" to "去年今天我们都记了什么？",
        "这个月记了多少" to "这个月我们记了多少条？给我一个概览",
        "最近的纪念日" to "最近有什么纪念日？还有几天？",
        "没归档的照片" to "有哪些照片还没归档？",
        "去过哪些城市" to "我们一起去过哪些城市？",
    )
    AppSheet(visible = true, onDismiss = onDismiss, title = "快捷提问") {
        Column(Modifier.fillMaxWidth()) {
            prompts.forEachIndexed { i, (label, text) ->
                if (i > 0) AppRowDivider(startIndent = 16.dp)
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clickable { onPick(text) }
                        .padding(horizontal = 16.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        label,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        "填入",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

package com.bookandmusic.lovelife.appnative.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Cake
import androidx.compose.material.icons.outlined.DarkMode
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import androidx.core.net.toUri
import com.bookandmusic.lovelife.appnative.data.ChatRepository
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppNavBar
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppNavItem
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppTopBar
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppTopBarAction
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppTopBarText
import com.bookandmusic.lovelife.appnative.designsystem.atoms.consumeTouches

/**
 * 五个 tab 与数据域一一对应：「我们」(关系/首页) 在首位并作为默认落地页，
 * 其后是 时光(动态流) / 相册(媒体库) / 纪念日 / 足迹(空间)。
 */
enum class MainTab(val label: String) {
    Us("我们"),
    Timeline("时光"),
    Albums("相册"),
    Anniversaries("纪念日"),
    Map("足迹"),
}

/** 壳内页面。internal 以便返回策略被单元测试覆盖（见 ShellBackTest）。 */
internal sealed interface Route {
    data class Main(val focusPostId: Long? = null) : Route
    data object Settings : Route
    data object Notifications : Route
    data object Anniversaries : Route
    data object About : Route
    data object AnniversaryForm : Route
    data object AlbumForm : Route
    data object PointForm : Route
    data object Publish : Route
    data class AlbumDetail(val id: Long, val name: String) : Route
    /** AI 对话全屏页（AI 模式的唯一形态；传统模式永不进入） */
    data object Chat : Route
}

/**
 * 返回目标：null 表示根页，不拦截系统返回（交给系统退出应用）。
 * 主壳与 AI 对话页都是根页——AI 对话页没有上级，落回传统主壳会让界面与 appMode 不一致。
 * 顶栏返回与系统返回共用这一张表，避免两条路径落到不同页面。
 * 逐个列出而不写 else：新增路由时编译器会强制在这里表态，避免又一个「根页」被当成二级页。
 */
internal fun backTarget(route: Route): Route? = when (route) {
    is Route.Main, Route.Chat -> null
    Route.AnniversaryForm -> Route.Anniversaries
    Route.About -> Route.Settings
    is Route.AlbumDetail -> Route.Main()
    Route.Settings, Route.Notifications, Route.Anniversaries,
    Route.Publish, Route.AlbumForm, Route.PointForm -> Route.Main()
}

/**
 * 应用壳：中性画布 + 玻璃顶栏（标题居中，铃铛/设置）
 * + 整页滚动内容 + 悬浮胶囊玻璃导航条（尾部角色按钮）。内容从顶栏下方滚过，玻璃才采得到东西。
 */
@Composable
fun DemoApp(
    dark: Boolean,
    themeMode: com.bookandmusic.lovelife.appnative.data.AppThemeMode,
    onThemeModeChange: (com.bookandmusic.lovelife.appnative.data.AppThemeMode) -> Unit,
    accent: com.bookandmusic.lovelife.appnative.designsystem.theme.AppAccent,
    onAccentChange: (com.bookandmusic.lovelife.appnative.designsystem.theme.AppAccent) -> Unit,
    onLogout: () -> Unit,
) {
    var tab by rememberSaveable { mutableStateOf(MainTab.Us) }
    // 模式是本机偏好：初值取上次选择，离开设置页时落盘（否则每次冷启动都掉回传统模式）
    val modeContext = androidx.compose.ui.platform.LocalContext.current
    // Route 是 sealed interface，不可进 Bundle：用 remember（旋转重建回主壳可接受）
    var route by remember {
        // 冷启动按持久化的模式落位：AI 模式直接进对话页，否则回到主壳
        mutableStateOf<Route>(
            if (com.bookandmusic.lovelife.appnative.data.ThemePreferences.appMode(modeContext) == "ai")
                Route.Chat else Route.Main()
        )
    }
    // 界面模式：classic | ai；设置页暂存、离开设置页生效
    var appMode by rememberSaveable {
        mutableStateOf(com.bookandmusic.lovelife.appnative.data.ThemePreferences.appMode(modeContext))
    }
    var pendingAppMode by rememberSaveable { mutableStateOf(appMode) }
    var aiAvailable by rememberSaveable { mutableStateOf(true) }
    var aiUnavailableReason by rememberSaveable { mutableStateOf("") }
    // 离开设置页 = 提交暂存模式并按新模式跳转（离开设置页才生效）。
    // 必须收成一条路：顶栏返回与系统返回键都走它，否则两处行为不一致，
    // 而「模式切换」只会在其中一条路上生效——真机上就是这个症状。
    fun leaveSettings() {
        appMode = pendingAppMode
        com.bookandmusic.lovelife.appnative.data.ThemePreferences.setAppMode(modeContext, pendingAppMode)
        route = if (pendingAppMode == "ai") Route.Chat else Route.Main()
    }

    // 二级页返回的唯一出口：设置页（含从「关于」回来后再退出）走 leaveSettings 收尾，
    // 其余回上级（关于 → 设置）。顶栏返回与系统返回键都调它，避免其中一条漏掉模式提交
    fun back() {
        if (route is Route.Settings) {
            leaveSettings()
            return
        }
        backTarget(route)?.let { route = it }
    }

    // AI 可用性：进入设置页/聊天页时拉一次（bootstrap 的 available+reason）。
    // key 必须带 route——只听 appMode 的话，从主壳进设置页不会重新触发，
    // aiAvailable 停在初值 true，「服务端没配 Key 就置灰」这条需求永远不生效。
    LaunchedEffect(appMode, route) {
        if (appMode == "ai" || route is Route.Settings) {
            runCatching { ChatRepository.bootstrap() }.onSuccess { b ->
                aiAvailable = b.available
                aiUnavailableReason = b.reason
            }
        }
    }
    var unread by remember { mutableIntStateOf(0) }
    val topAction = rememberTopBarActionHost()
    // 启动 / 登录后各检查一次更新；重入由控制器拦掉
    val updateContext = androidx.compose.ui.platform.LocalContext.current
    LaunchedEffect(Unit) { com.bookandmusic.lovelife.appnative.update.UpdateController.check(updateContext) }
    LaunchedEffect(route) {
        unread = com.bookandmusic.lovelife.appnative.data.NotificationsRepository.unreadCount()
    }

    val statusTop = WindowInsets.statusBars.asPaddingValues().calculateTopPadding()
    val navBottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
    // 内容内边距：顶部让开「状态栏 + 44dp 标题行 + 8dp」。
    // 底部 = 手势区 + 导航条总高 68（heightIn 58 + 上下 padding 5+5）+ 距底 8 + 内容呼吸 12。
    // 漏算导航条自身的 10dp 垂直 padding 会让末条内容被压住（列表页最后一项显示不全）。
    val navBarHeight = 68.dp
    val contentTop = statusTop + 44.dp + 8.dp
    val contentBottom = navBottom + navBarHeight + 8.dp + 12.dp
    val mainPadding = PaddingValues(top = contentTop, bottom = contentBottom)
    // 设置 / 通知不属于任何 tab（导航条没有可高亮的项）→ 这两页不显示底部导航，
    // 底部留白随之只让出手势区，不给导航条预留，否则页面尾部会空一大块。
    val hideNavBar = route is Route.Settings || route is Route.Notifications || route is Route.Chat
    val subPadding = PaddingValues(
        top = contentTop,
        bottom = if (hideNavBar) navBottom + 12.dp else contentBottom,
    )
    // AI 页没有壳层顶栏，顶部只让开状态栏（板内自带顶栏）
    val chatPadding = PaddingValues(top = statusTop, bottom = navBottom)

    androidx.compose.runtime.CompositionLocalProvider(LocalTopBarAction provides topAction) {
        Box(
            Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background)
        ) {
            Box(Modifier.fillMaxSize()) {
                when (val r = route) {
                    is Route.Main -> MainTabs(
                        tab = tab,
                        contentPadding = mainPadding,
                        onSelectTab = { tab = it },
                        onOpenAnniversaries = {
                            tab = MainTab.Anniversaries
                            route = Route.Main()
                        },
                        onOpenAnniversaryForm = { route = Route.AnniversaryForm },
                        onOpenAlbumForm = { route = Route.AlbumForm },
                        onOpenPointForm = { route = Route.PointForm },
                        onOpenPublish = { route = Route.Publish },
                        onOpenAlbum = { id, name -> route = Route.AlbumDetail(id, name) },
                        focusPostId = r.focusPostId,
                        onFocusConsumed = { route = Route.Main() },
                        onOpenPost = { id ->
                            tab = MainTab.Timeline
                            route = Route.Main(focusPostId = id)
                        },
                    )
                    Route.Settings -> SettingsScreen(
                        contentPadding = subPadding,
                        themeMode = themeMode,
                        onThemeModeChange = onThemeModeChange,
                        accent = accent,
                        onAccentChange = onAccentChange,
                        onOpenAbout = { route = Route.About },
                        onLogout = {
                            onLogout()
                            route = Route.Main()
                        },
                        appMode = appMode,
                        pendingAppMode = pendingAppMode,
                        onPendingModeChange = { pendingAppMode = it },
                        aiAvailable = aiAvailable,
                        aiUnavailableReason = aiUnavailableReason,
                    )
                    Route.Chat -> ChatScreen(
                        contentPadding = chatPadding,
                        aiAvailable = aiAvailable,
                        aiUnavailableReason = aiUnavailableReason,
                        onBackToClassic = {
                            // AI 页顶栏选「传统模式」：直接切换并落盘（否则冷启动又回 AI）
                            appMode = "classic"; pendingAppMode = "classic"
                            com.bookandmusic.lovelife.appnative.data.ThemePreferences.setAppMode(modeContext, "classic")
                            route = Route.Main()
                        },
                        onOpenSettings = { route = Route.Settings },
                    )
                    Route.Notifications -> NotificationsScreen(
                        contentPadding = subPadding,
                        onBack = { route = Route.Main() },
                        onOpenTarget = { type, id, snapshot ->
                            // 点击通知跳转对应内容。五类 target_type 全部覆盖：
                            // post / comment → 时光页定位高亮；album → 相册详情；anniversary / map_point → 对应 tab
                            when (type) {
                                "post" -> {
                                    tab = MainTab.Timeline
                                    route = Route.Main(focusPostId = id)
                                }
                                "comment" -> {
                                    // 评论删除后评论已不可见，跳到它所属的动态（快照里的 post_id）
                                    val postId = snapshotPostId(snapshot) ?: id
                                    tab = MainTab.Timeline
                                    route = Route.Main(focusPostId = postId)
                                }
                                "album" -> {
                                    tab = MainTab.Albums
                                    route = Route.AlbumDetail(id, "")
                                }
                                "anniversary" -> {
                                    tab = MainTab.Anniversaries
                                    route = Route.Main()
                                }
                                "map_point" -> {
                                    tab = MainTab.Map
                                    route = Route.Main()
                                }
                                // 内容删除通知无跳转（内容已不可见）：只消费掉通知
                                else -> Unit
                            }
                        },
                    )
                    Route.About -> {
                        val ctx = androidx.compose.ui.platform.LocalContext.current
                        AboutScreen(
                            contentPadding = subPadding,
                            onOpenUrl = { url ->
                                runCatching {
                                    ctx.startActivity(
                                        android.content.Intent(
                                            android.content.Intent.ACTION_VIEW,
                                            url.toUri(),
                                        )
                                    )
                                }
                            },
                        )
                    }
                    Route.Anniversaries -> AnniversaryListScreen(
                        contentPadding = subPadding,
                        onBack = { route = Route.Main() },
                        onAdd = { route = Route.AnniversaryForm },
                    )
                    Route.AnniversaryForm -> AnniversaryFormScreen(
                        contentPadding = subPadding,
                        onBack = { route = Route.Anniversaries },
                        onSaved = { route = Route.Anniversaries },
                    )
                    Route.AlbumForm -> AlbumFormScreen(
                        contentPadding = subPadding,
                        onBack = { route = Route.Main() },
                        onSaved = { id, name ->
                            tab = MainTab.Albums
                            route = Route.AlbumDetail(id, name)
                        },
                    )
                    Route.PointForm -> PointFormScreen(
                        contentPadding = subPadding,
                        onBack = { route = Route.Main() },
                        onSaved = {
                            tab = MainTab.Map
                            route = Route.Main()
                        },
                    )
                    Route.Publish -> PublishScreen(
                        contentPadding = subPadding,
                        onBack = { route = Route.Main() },
                        onPublished = {
                            tab = MainTab.Timeline
                            route = Route.Main()
                        },
                    )
                    is Route.AlbumDetail -> AlbumDetailScreen(
                        albumId = r.id,
                        albumName = r.name,
                        contentPadding = subPadding,
                        onBack = { route = Route.Main() },
                    )
                }
            }
            // 顶栏：主壳用当前 tab 名 + 铃铛/深浅/设置；二级页用页名 + 返回。
            // **AI 对话页不渲染壳层顶栏**：它是与传统模式并列的「模式」，不是二级页——
            // 页面自带完整顶栏（会话抽屉 / 标题 / 设置），叠两条就是截图里那个双层头。
            val shellTopBar = route !is Route.Chat
            val title = when (val r = route) {
                is Route.Main -> tab.label
                Route.Chat -> "我们的 AI 助手"
                Route.Settings -> "设置"
                Route.About -> "关于"
                Route.Notifications -> "通知"
                Route.Anniversaries -> "纪念日"
                Route.AnniversaryForm -> "添加纪念日"
                Route.AlbumForm -> "新建相册"
                Route.PointForm -> "添加地点"
                Route.Publish -> "记动态"
                is Route.AlbumDetail -> r.name
            }
            if (shellTopBar) AppTopBar(
                title = title,
                modifier = Modifier.align(Alignment.TopCenter),
                leading = if (route is Route.Main) {
                    null
                } else {
                    {
                        AppTopBarAction(
                            Icons.AutoMirrored.Outlined.ArrowBack,
                            "返回",
                            onClick = {
                                // 设置页的返回要提交模式切换，其余页只是回到上级
                                back()
                            },
                            tint = MaterialTheme.colorScheme.primary,
                        )
                    }
                },
                trailing = {
                    if (route is Route.Main) {
                        // 顶栏动作按页分工：首页只给「通知 / 设置」（回顾页，无新增对象）；
                        // 其余四页只给「+」（新增入口就近放，不重复放通知与设置）。
                        if (tab == MainTab.Us) {
                            AppTopBarAction(
                                Icons.Outlined.Notifications,
                                "通知",
                                onClick = { route = Route.Notifications },
                                badgeCount = unread,
                            )
                            AppTopBarAction(Icons.Outlined.Settings, "设置", onClick = { route = Route.Settings })
                        } else {
                            addActionFor(tab)?.let { addAction ->
                                AppTopBarAction(
                                    Icons.Outlined.Add,
                                    addAction.second,
                                    onClick = { route = addAction.first },
                                )
                            }
                        }
                    } else if (topAction.icon != null) {
                        AppTopBarAction(
                            topAction.icon!!,
                            "添加",
                            onClick = { topAction.onClick?.invoke() },
                            tint = MaterialTheme.colorScheme.primary,
                        )
                    } else if (topAction.text != null) {
                        AppTopBarText(
                            text = topAction.text.orEmpty(),
                            enabled = topAction.enabled,
                            onClick = { topAction.onClick?.invoke() },
                        )
                    }
                },
            )
            // 底部导航常驻主壳与二级页（设置 / 通知除外，见 hideNavBar）：点 tab 直接回主壳并切域
            if (!hideNavBar) run {
                // 外层 Box 吃掉整条底部区域（含胶囊下方留白）的触摸：
                // 否则点导航条边缘会穿透到下面滚动的列表
                Box(
                    Modifier
                        .align(Alignment.BottomCenter)
                        .fillMaxWidth()
                        .consumeTouches()
                ) {
                AppNavBar(
                    items = MainTab.entries.map { AppNavItem(it.label, icon(it)) },
                    selected = tab.ordinal,
                    onSelect = {
                        tab = MainTab.entries[it]
                        route = Route.Main()
                    },
                    modifier = Modifier.align(Alignment.BottomCenter),
                )
                }
            }
        }
    }
    // 系统返回键：与顶栏返回走同一条路径（设置页特殊，见 leaveSettings）；
    // 根页（主壳 / AI 对话页）不拦截，让系统正常退出应用。
    BackHandler(enabled = backTarget(route) != null) { back() }
}

@Composable
private fun MainTabs(
    tab: MainTab,
    contentPadding: PaddingValues,
    onSelectTab: (MainTab) -> Unit,
    onOpenAnniversaries: () -> Unit,
    onOpenAnniversaryForm: () -> Unit,
    onOpenAlbumForm: () -> Unit,
    onOpenPointForm: () -> Unit,
    onOpenPublish: () -> Unit,
    onOpenAlbum: (Long, String) -> Unit,
    focusPostId: Long? = null,
    onFocusConsumed: () -> Unit = {},
    onOpenPost: (Long) -> Unit = {},
) {
    when (tab) {
        MainTab.Timeline -> TimelineScreen(
            contentPadding = contentPadding,
            onOpenPublish = onOpenPublish,
            focusPostId = focusPostId,
            onFocusConsumed = onFocusConsumed,
        )
        MainTab.Albums -> AlbumsScreen(
            contentPadding = contentPadding,
            onOpenAlbum = onOpenAlbum,
            onOpenAlbumForm = onOpenAlbumForm,
            onOpenPublish = onOpenPublish,
        )
        MainTab.Anniversaries -> AnniversaryListScreen(
            contentPadding = contentPadding,
            onBack = {},
            onAdd = onOpenAnniversaryForm,
            inTab = true,
        )
        MainTab.Map -> MapScreen(
            contentPadding = contentPadding,
            onOpenPointForm = onOpenPointForm,
        )
        MainTab.Us -> UsScreen(
            contentPadding = contentPadding,
            onOpenPost = onOpenPost,
            onOpenAnniversaries = onOpenAnniversaries,
            onOpenPublish = onOpenPublish,
            onOpenTimeline = { onSelectTab(MainTab.Timeline) },
        )
    }
}

/** 导航条尾部角色按钮：主色「+」+ 当前 tab 的添加含义。 */
/**
 * 从通知快照 JSON 里取 post_id（评论删除通知用）。
 * 快照是服务端写入的自由 JSON，解析失败按无值处理，不抛给 UI。
 */
private fun snapshotPostId(snapshot: String): Long? = runCatching {
    val json = kotlinx.serialization.json.Json.parseToJsonElement(snapshot)
    (json as? kotlinx.serialization.json.JsonObject)
        ?.get("post_id")
        ?.let { (it as? kotlinx.serialization.json.JsonPrimitive)?.content?.toLongOrNull() }
}.getOrNull()

/**
 * 各 tab 的「+」动作（Route + 无障碍文案）。返回 null = 该页不显示「+」。
 * 顶栏是唯一新增入口；导航条只做页面切换。
 */
private fun addActionFor(tab: MainTab): Pair<Route, String>? = when (tab) {
    // 首页不放「+」：它是回顾页，新增入口在各自的数据页
    MainTab.Us -> null
    MainTab.Timeline -> Route.Publish to "记动态"
    MainTab.Albums -> Route.AlbumForm to "新建相册"
    MainTab.Anniversaries -> Route.AnniversaryForm to "添加纪念日"
    MainTab.Map -> Route.PointForm to "添加地点"
}


private fun icon(tab: MainTab) = when (tab) {
    MainTab.Us -> Icons.Outlined.FavoriteBorder
    MainTab.Timeline -> Icons.Outlined.History
    MainTab.Albums -> Icons.Outlined.PhotoLibrary
    MainTab.Anniversaries -> Icons.Outlined.Cake
    MainTab.Map -> Icons.Outlined.Place
}

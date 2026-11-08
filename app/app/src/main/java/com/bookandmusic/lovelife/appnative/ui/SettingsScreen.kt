package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.ui.draw.alpha
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.DarkMode
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Logout
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Update
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.AppThemeMode
import com.bookandmusic.lovelife.appnative.data.AuthRepository
import com.bookandmusic.lovelife.appnative.data.StylesRepository
import com.bookandmusic.lovelife.appnative.data.errText
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppActionSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMenuItem
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadge
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadgeSize
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppToast
import com.bookandmusic.lovelife.appnative.designsystem.theme.AppAccent
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.update.UpdateController
import com.bookandmusic.lovelife.appnative.update.UpdateStage
import kotlinx.coroutines.launch

/**
 * 设置页：**按模式分层、一级只放常用、低频项收二级页**。
 *
 * 层级：外观（深浅三档 + 强调色）/ AI 偏好 / 检查更新 / 关于 / 退出登录。
 * 资料与空间配置不在本页（由后台维护）；回收站、模型配置、审计也不在本页。
 */
@Composable
fun SettingsScreen(
    contentPadding: androidx.compose.foundation.layout.PaddingValues,
    themeMode: AppThemeMode,
    onThemeModeChange: (AppThemeMode) -> Unit,
    accent: AppAccent,
    onAccentChange: (AppAccent) -> Unit,
    onLogout: () -> Unit,
    onOpenAbout: () -> Unit,
    /** 模式行：宿主持有 appMode/pendingAppMode，离开设置页才生效 */
    appMode: String = "classic",
    pendingAppMode: String = appMode,
    onPendingModeChange: (String) -> Unit = {},
    aiAvailable: Boolean = true,
    aiUnavailableReason: String = "",
) {
    val colors = LocalAppColors.current
    var showThemeSheet by remember { mutableStateOf(false) }
    var showStyleSheet by remember { mutableStateOf(false) }
    var showModeSheet by remember { mutableStateOf(false) }
    var toast by remember { mutableStateOf<String?>(null) }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // 检查更新行显示真实状态（一级行内触发 + 右侧显示检查 / 下载状态）
    val update by UpdateController.state.collectAsState()
    // 手动检查的结果提示：一次性消费后清掉，避免离开再回来重复弹
    LaunchedEffect(update.manualNotice) {
        if (update.manualNotice.isNotEmpty()) {
            toast = update.manualNotice
            UpdateController.clearNotice()
        }
    }
    // 输出风格：服务端个人偏好（跨设备生效）。名字来自风格库，当前值来自 /auth/me
    var aiStyle by remember { mutableStateOf("") }
    var aiStyles by remember { mutableStateOf(listOf("文艺", "幽默", "简洁")) }
    LaunchedEffect(Unit) {
        aiStyles = runCatching { StylesRepository.names() }.getOrDefault(emptyList())
            .ifEmpty { listOf("文艺", "幽默", "简洁") }
        aiStyle = runCatching { AuthRepository.restore()?.ai_style }.getOrNull().orEmpty()
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            // 壳层统一内边距：底部让开悬浮导航条
            .padding(contentPadding)
            .padding(horizontal = 16.dp)
            .padding(top = 8.dp)
    ) {
        // 模式行：选定只暂存，离开设置页才生效
        SettingSection("模式") {
            SettingRow(
                icon = Icons.Outlined.AutoAwesome,
                title = "界面模式",
                value = if (pendingAppMode == "ai") "AI 模式" else "传统模式",
                onClick = { showModeSheet = true },
            )
        }

        SettingSection("外观") {
            SettingRow(
                icon = Icons.Outlined.DarkMode,
                title = "深浅模式",
                value = themeMode.label,
                onClick = { showThemeSheet = true },
            )
            AccentPickerRow(selected = accent, onSelect = onAccentChange)
        }

        // AI 偏好：输出风格（个人体验项；仅 AI 模式下有实际效果）
        SettingSection("AI 偏好") {
            SettingRow(
                icon = Icons.Outlined.AutoAwesome,
                title = "输出风格",
                value = aiStyle.ifEmpty { "默认（文艺）" },
                onClick = { showStyleSheet = true },
            )
        }

        SettingSection("通用") {
            SettingRow(
                icon = Icons.Outlined.Update,
                title = "检查更新",
                value = when {
                    update.stage == UpdateStage.Checking -> "检查中…"
                    update.stage == UpdateStage.Downloading -> "下载中 ${update.percent}%"
                    update.stage == UpdateStage.Ready || update.stage == UpdateStage.Handoff ->
                        "v${update.release?.versionName.orEmpty()} 待安装"
                    update.stage == UpdateStage.Failed -> "下载失败"
                    // 弹着「发现新版本」时这行必须说可更新——否则用户看到「已是最新」和弹窗自相矛盾
                    update.stage == UpdateStage.Prompt -> "v${update.release?.versionName.orEmpty()} 可更新"
                    // 点过「稍后」但包已经下好：不能显示「已是最新」，否则用户再也找不到那个包
                    UpdateController.hasCachedPackage() -> "v${update.release?.versionName.orEmpty()} 待安装"
                    else -> "已是最新"
                },
                onClick = {
                    val stage = UpdateController.state.value.stage
                    // 已有会话（下载中 / 待安装 / 失败 / 已交接）→ 重新打开弹窗，而不是再查一次
                    if (stage == UpdateStage.Downloading || stage == UpdateStage.Ready ||
                        stage == UpdateStage.Failed || stage == UpdateStage.Handoff
                    ) {
                        UpdateController.openSheet()
                    } else {
                        // 含「点过稍后、包已下好」的情形：手动检查走 Ready 分支，直接给「立即安装」
                        scope.launch { UpdateController.check(context, manual = true) }
                    }
                },
            )
            SettingRow(
                icon = Icons.Outlined.Info,
                title = "关于",
                value = null,
                onClick = onOpenAbout,
            )
        }

        // 退出登录（固定垫底）
        SettingSection(null) {
            Row(
                Modifier
                    .fillMaxWidth()
                    .clickable { onLogout() }
                    .padding(horizontal = 16.dp, vertical = 16.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Icon(
                    Icons.Outlined.Logout,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.error,
                    modifier = Modifier.size(22.dp),
                )
                Text(
                    "退出登录",
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
        Spacer(Modifier.height(16.dp))
    }

    if (showModeSheet) {
        ModePickerSheet(
            current = pendingAppMode,
            aiAvailable = aiAvailable,
            aiUnavailableReason = aiUnavailableReason,
            onDismiss = { showModeSheet = false },
            onPick = { m ->
                onPendingModeChange(m)
                showModeSheet = false
            },
        )
    }

if (showThemeSheet) {
        AppActionSheet(
            visible = true,
            title = "深浅模式",
            items = AppThemeMode.entries.map { mode ->
                AppMenuItem(
                    label = if (mode == themeMode) "${mode.label}（当前）" else mode.label,
                    icon = Icons.Outlined.DarkMode,
                ) {
                    onThemeModeChange(mode)
                    showThemeSheet = false
                }
            },
            onDismiss = { showThemeSheet = false },
        )
    }

    if (showStyleSheet) {
        AppActionSheet(
            visible = true,
            title = "输出风格",
            items = aiStyles.map { style ->
                AppMenuItem(
                    label = if (style == aiStyle) "$style（当前）" else style,
                    icon = Icons.Outlined.AutoAwesome,
                ) {
                    showStyleSheet = false
                    scope.launch {
                        runCatching { AuthRepository.updateAiStyle(style) }
                            .onSuccess {
                                aiStyle = style
                                toast = "输出风格已切换为$style"
                            }
                            .onFailure { toast = "切换失败：${errText(it, "")}" }
                    }
                }
            },
            onDismiss = { showStyleSheet = false },
        )
    }

    AppToast(text = toast, onDismiss = { toast = null })
}

/** 强调色预设选择（暖色系，点到即生效，本地保存）。 */
@Composable
private fun AccentPickerRow(selected: AppAccent, onSelect: (AppAccent) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            "强调色",
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            AppAccent.entries.forEach { acc ->
                val isSelected = acc == selected
                Box(
                    Modifier
                        .size(26.dp)
                        .clip(CircleShape)
                        .background(acc.color)
                        .then(
                            if (isSelected) {
                                Modifier.border(2.dp, MaterialTheme.colorScheme.onSurface, CircleShape)
                            } else {
                                Modifier.border(1.dp, MaterialTheme.colorScheme.outlineVariant, CircleShape)
                            }
                        )
                        .clickable { onSelect(acc) },
                    contentAlignment = Alignment.Center,
                ) {
                    if (isSelected) {
                        Icon(
                            Icons.Filled.Check,
                            contentDescription = acc.label,
                            tint = MaterialTheme.colorScheme.onPrimary,
                            modifier = Modifier.size(15.dp),
                        )
                    }
                }
            }
        }
    }
}

/**
 * 设置分组：组标题（可空）+ 浅灰底块。
 * 不用卡片容器（无边框无阴影）：靠底色区分分组，与微信「我-设置」一致。
 */
@Composable
private fun SettingSection(title: String?, content: @Composable () -> Unit) {
    Column(Modifier.padding(bottom = 16.dp)) {
        if (title != null) {
            Text(
                title,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
            )
        }
        Column(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.surface)
        ) {
            content()
        }
    }
}

/** 设置行：图标块 + 标题 + 值 + chevron。 */
@Composable
private fun SettingRow(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    title: String,
    value: String? = null,
    onClick: () -> Unit,
) {
    val colors = LocalAppColors.current
    Row(
        Modifier
            .fillMaxWidth()
            .clickable { onClick() }
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.size(22.dp),
        )
        Text(
            title,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
        if (value != null) {
            Text(
                value,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Icon(
            Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.outline,
            modifier = Modifier.size(18.dp),
        )
    }
}


/** 模式选择浮层：两张带界面摘要的选项卡；AI 不可用置灰并提示原因。 */
@Composable
private fun ModePickerSheet(
    current: String,
    aiAvailable: Boolean,
    aiUnavailableReason: String,
    onDismiss: () -> Unit,
    onPick: (String) -> Unit,
) {
    com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSheet(
        visible = true,
        onDismiss = onDismiss,
        title = "界面模式",
    ) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
            ModeOptionCard(
                selected = current != "ai",
                title = "传统模式",
                desc = "五页浏览 + 手动操作，微信式的熟悉界面",
                enabled = true,
                reason = "",
                onClick = { onPick("classic") },
            )
            Spacer(Modifier.height(10.dp))
            ModeOptionCard(
                selected = current == "ai",
                title = "AI 模式",
                desc = "全屏对话，用一句话完成记录与查询",
                enabled = aiAvailable,
                reason = if (aiAvailable) "" else (aiUnavailableReason.ifEmpty { "AI 暂不可用" }),
                onClick = { if (aiAvailable) onPick("ai") },
            )
            Spacer(Modifier.height(12.dp))
            Text(
                "选定后返回即生效",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.align(androidx.compose.ui.Alignment.CenterHorizontally),
            )
        }
    }
}

@Composable
private fun ModeOptionCard(
    selected: Boolean,
    title: String,
    desc: String,
    enabled: Boolean,
    reason: String,
    onClick: () -> Unit,
) {
    val colors = LocalAppColors.current
    Column(
        Modifier
            .fillMaxWidth()
            .clip(androidx.compose.foundation.shape.RoundedCornerShape(12.dp))
            .background(
                if (selected) MaterialTheme.colorScheme.primaryContainer
                else MaterialTheme.colorScheme.surfaceContainerHigh
            )
            .then(if (enabled) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 16.dp, vertical = 12.dp)
            .alpha(if (enabled) 1f else 0.55f),
    ) {
        Text(
            title,
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            desc,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 2.dp),
        )
        if (reason.isNotEmpty()) {
            Text(
                reason,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}

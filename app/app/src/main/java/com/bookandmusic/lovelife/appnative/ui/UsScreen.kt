package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Cake
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.painterResource
import com.bookandmusic.lovelife.appnative.R
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.data.PostUi
import com.bookandmusic.lovelife.appnative.data.PostsRepository
import com.bookandmusic.lovelife.appnative.data.SpaceInfoDto
import com.bookandmusic.lovelife.appnative.data.SpaceRepository
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppCard
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppEmptyState
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconAction
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadge
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadgeSize
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSectionTitle
import com.bookandmusic.lovelife.appnative.designsystem.theme.AppCardBodyStyle
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppDark
import com.bookandmusic.lovelife.appnative.util.TimeFmt

/**
 * 我们 = 关系域：头卡（叠头像 + 大天数 + 下一纪念日）→ 纪念日预览三行
 * → 回忆精选（封面大图 + 文字回忆）→ 那年今日。整页滚动，卡片白底细边。
 */
@Composable
fun UsScreen(
    contentPadding: PaddingValues,
    onOpenAnniversaries: () -> Unit,
    onOpenPublish: () -> Unit,
    onOpenTimeline: () -> Unit = {},
    /** 点回忆卡 → 定位到该条动态 */
    onOpenPost: (Long) -> Unit = {},
) {
    var space by remember { mutableStateOf<SpaceInfoDto?>(null) }
    var mediaPosts by remember { mutableStateOf<List<PostUi>>(emptyList()) }
    var textPosts by remember { mutableStateOf<List<PostUi>>(emptyList()) }
    var memories by remember { mutableStateOf<List<PostUi>>(emptyList()) }
    var shuffleTick by remember { mutableIntStateOf(0) }

    suspend fun reload() {
        space = runCatching { SpaceRepository.info() }.getOrNull()
        mediaPosts = runCatching { PostsRepository.sampled(hasMedia = true) }.getOrDefault(emptyList())
        textPosts = runCatching { PostsRepository.sampled(hasMedia = false) }.getOrDefault(emptyList())
        memories = runCatching { PostsRepository.memories() }.getOrDefault(emptyList())
    }

    LaunchedEffect(shuffleTick) { reload() }

    val cover = remember(mediaPosts, shuffleTick) {
        mediaPosts.filter { it.media.isNotEmpty() }.shuffled().firstOrNull()
    }
    val textPicks = remember(textPosts, shuffleTick) { textPosts.shuffled().take(2) }
    val todayMemories = remember(memories) { memories.filter { isTodayInHistory(it.createdAt) } }

    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = contentPadding,
    ) {
        item { HeroCard(space, onOpenAnniversaries) }
        item {
            Column(Modifier.padding(horizontal = 16.dp).padding(top = 18.dp)) {
                AppSectionTitle(
                    title = "回忆精选",
                    action = "更多",
                    onAction = onOpenTimeline,
                    trailing = {
                        AppIconAction(Icons.Outlined.Refresh, "换一批", onClick = { shuffleTick++ })
                    },
                )
                when {
                    cover != null -> CoverMemoryCard(cover, onClick = { onOpenPost(cover.id) })
                    textPicks.isEmpty() -> AppEmptyState(
                        title = "回忆从今天开始",
                        hint = "写下第一条，往后每个今天都值得回看。",
                        icon = Icons.Outlined.History,
                        cta = "写第一条",
                        onCta = onOpenPublish,
                    )
                }
                textPicks.forEach { m -> TextMemoryCard(m, onClick = { onOpenPost(m.id) }) }
            }
        }
        if (todayMemories.isNotEmpty()) {
            item {
                Row(
                    Modifier
                        .padding(horizontal = 16.dp)
                        .padding(top = 8.dp)
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .clickable { todayMemories.firstOrNull()?.let { onOpenPost(it.id) } ?: onOpenTimeline() }
                        .padding(horizontal = 14.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        "那年今日 · ${todayMemories.size} 篇",
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    Icon(
                        Icons.AutoMirrored.Filled.KeyboardArrowRight,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.outline,
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
        item { Spacer(Modifier.size(8.dp)) }
    }
}

private fun isTodayInHistory(raw: String): Boolean {
    val d = TimeFmt.parse(raw) ?: return false
    val today = java.time.LocalDate.now()
    return d.monthValue == today.monthValue && d.dayOfMonth == today.dayOfMonth && d.year < today.year
}

/**
 * 关系身份带（首页页头）：樱花插画 200dp + 纵向铺开的三层信息。
 *
 * 空间分配（200dp）：上留白 20 → 头像 56 → 8 → 天数 44 → 8 → 纪念日胶囊 34 → 20。
 * 信息从上往下铺满整条带子，不全挤在底部；底边只留 32dp 渐隐，不覆盖文字。
 */
@Composable
private fun HeroCard(space: SpaceInfoDto?, onOpenAnniversaries: () -> Unit) {
    val colors = LocalAppColors.current
    val dark = LocalAppDark.current
    val members = space?.members.orEmpty()
    val her = members.firstOrNull { it.role == "girl" } ?: members.getOrNull(0)
    val him = members.firstOrNull { it.role == "boy" } ?: members.getOrNull(1)
    val names = members.take(2).map { it.nickname }.joinToString(" & ").ifEmpty { "我们的空间" }
    val next = space?.next_anniversary
    val hasDays = space?.days_together != null && space?.love_start_date != null

    Box(
        Modifier
            .fillMaxWidth()
            .height(200.dp)
    ) {
        Image(
            painter = painterResource(if (dark) R.drawable.bg_home_dark else R.drawable.bg_home_light),
            contentDescription = null,
            contentScale = ContentScale.Crop,
            alignment = Alignment.CenterEnd,
            modifier = Modifier.fillMaxSize(),
        )
        // 底边 32dp 渐隐：只做与画布的过渡，不压到文字
        Box(
            Modifier
                .fillMaxWidth()
                .height(32.dp)
                .align(Alignment.BottomCenter)
                .background(
                    Brush.verticalGradient(
                        listOf(
                            MaterialTheme.colorScheme.background.copy(alpha = 0f),
                            MaterialTheme.colorScheme.background,
                        )
                    )
                )
        )
        Column(
            Modifier
                .fillMaxSize()
                .padding(start = 20.dp, end = 20.dp, top = 20.dp, bottom = 20.dp)
        ) {
            // ① 双人头像 + 昵称（同一行，头像 56dp 是页头锚点）
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(width = 96.dp, height = 56.dp)) {
                    Box { AvatarBlock(him?.nickname, him?.role, him?.avatar, size = 56.dp) }
                    Box(Modifier.offset(x = 40.dp)) {
                        AvatarBlock(her?.nickname, her?.role, her?.avatar, size = 56.dp)
                    }
                }
                Column(Modifier.padding(start = 14.dp).weight(1f)) {
                    Text(
                        names,
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (space?.love_start_date != null) {
                        Text(
                            "${TimeFmt.dotted(space.love_start_date)} 开始",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            // ② 在一起天数（页头最强视觉重量）
            if (hasDays) {
                Row(
                    Modifier.padding(top = 10.dp),
                    verticalAlignment = Alignment.Bottom,
                ) {
                    Text(
                        space!!.days_together.toString(),
                        style = MaterialTheme.typography.displaySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        "天",
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(start = 4.dp, bottom = 8.dp),
                    )
                    Text(
                        "在一起",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(start = 8.dp, bottom = 10.dp),
                    )
                }
            }
            Spacer(Modifier.weight(1f))
            // ③ 下一个纪念日（宽度随内容，不占满整宽——它不是页头主角）
            if (next != null && next.name.isNotEmpty()) {
                Row(
                    Modifier
                        .clip(RoundedCornerShape(999.dp))
                        .background(MaterialTheme.colorScheme.primary)
                        .clickable { onOpenAnniversaries() }
                        .padding(start = 14.dp, end = 10.dp, top = 7.dp, bottom = 7.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(
                        Icons.Outlined.Cake,
                        contentDescription = null,
                        tint = colors.onSolid,
                        modifier = Modifier.size(15.dp),
                    )
                    Text(
                        next.name,
                        style = MaterialTheme.typography.bodyMedium,
                        color = colors.onSolid,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(start = 7.dp),
                    )
                    Text(
                        "还有 ${next.days_until} 天",
                        style = MaterialTheme.typography.titleMedium,
                        color = colors.onSolid,
                        modifier = Modifier.padding(start = 10.dp),
                    )
                    Icon(
                        Icons.AutoMirrored.Filled.KeyboardArrowRight,
                        contentDescription = null,
                        tint = colors.onSolid.copy(alpha = 0.9f),
                        modifier = Modifier.padding(start = 2.dp).size(17.dp),
                    )
                }
            }
        }
    }
}

/** 52dp 圆形头像：有图加载图，无图显示首字。 */
@Composable
private fun AvatarBlock(nickname: String?, role: String?, avatar: String?, size: androidx.compose.ui.unit.Dp = 52.dp) {
    val colors = LocalAppColors.current
    val bg = if (role == "boy") colors.semBlue else MaterialTheme.colorScheme.primary
    Box(
        Modifier
            .size(size)
            .clip(CircleShape)
            .background(bg)
            .border(2.5.dp, Color.White.copy(alpha = 0.85f), CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        if (!avatar.isNullOrEmpty()) {
            AsyncImage(
                model = Channel.base().trimEnd('/') + avatar,
                contentDescription = nickname,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Text(
                nickname?.take(1) ?: "·",
                style = MaterialTheme.typography.headlineSmall,
                color = colors.onSolid,
            )
        }
    }
}

@Composable
private fun CoverMemoryCard(post: PostUi, onClick: () -> Unit = {}) {
    val colors = LocalAppColors.current
    val url = post.media.firstOrNull()?.thumbUrl
    Box(
        Modifier
            .fillMaxWidth()
            .padding(top = 12.dp)
            .clip(RoundedCornerShape(20.dp))
            .clickable { onClick() }
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
    ) {
        AsyncImage(
            model = url,
            contentDescription = null,
            contentScale = ContentScale.Crop,
            modifier = Modifier.fillMaxWidth().aspectRatio(16f / 10f),
        )
        Column(
            Modifier
                .matchParentSize()
                .background(
                    Brush.verticalGradient(
                        0.42f to Color.Transparent,
                        1f to Color.Black.copy(alpha = 0.78f),
                    )
                )
                .padding(16.dp),
            verticalArrangement = Arrangement.Bottom,
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.padding(bottom = 6.dp)) {
                post.mood?.let { Tag(it, Icons.Outlined.AutoAwesome, colors.onSolid) }
                post.locationName?.let { Tag(it, Icons.Outlined.Place, colors.onSolid) }
            }
            Text(
                post.content.ifEmpty { "一条回忆" }.take(24),
                style = MaterialTheme.typography.headlineSmall,
                color = colors.onSolid,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                TimeFmt.date(post.createdAt),
                style = MaterialTheme.typography.labelMedium,
                color = colors.onSolid.copy(alpha = 0.88f),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

@Composable
private fun Tag(text: String, icon: androidx.compose.ui.graphics.vector.ImageVector, tint: Color) {
    Row(
        Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(Color.White.copy(alpha = 0.22f))
            .padding(horizontal = 8.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(12.dp))
        Text(text, style = MaterialTheme.typography.labelMedium, color = tint)
    }
}

/** 文字回忆（无卡片容器）：左侧 3dp 主色竖条 + 两行正文 + 日期·心情·地点。 */
@Composable
private fun TextMemoryCard(post: PostUi, onClick: () -> Unit = {}) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(top = 10.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(MaterialTheme.colorScheme.surface)
            .clickable { onClick() }
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
            Box(
                Modifier
                    .size(width = 3.dp, height = 44.dp)
                    .clip(RoundedCornerShape(999.dp))
                    .background(MaterialTheme.colorScheme.primary)
            )
            Column(Modifier.weight(1f)) {
                Text(
                    post.content.ifEmpty { "一条回忆" },
                    style = AppCardBodyStyle,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    buildString {
                        append(TimeFmt.date(post.createdAt))
                        post.mood?.let { append(" · $it") }
                        post.locationName?.let { append(" · $it") }
                    },
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(top = 6.dp),
                )
        }
    }
}

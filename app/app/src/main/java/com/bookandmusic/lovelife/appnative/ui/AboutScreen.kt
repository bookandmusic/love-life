package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.R
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.brandTileShape

/**
 * 项目主页：静态关于页的外链，不是数据通道——不随环境变化、不经服务端下发。
 * 展示文本与打开地址共用这一个常量，避免两处各写一份。
 */
private const val PROJECT_URL = "https://github.com/bookandmusic/love-life"

/**
 * 关于（二级页）：版本号 / 服务端地址 / 项目主页。
 *
 * 结构：顶部品牌区（logo + 名称 + slogan + 版本，居中）→ 信息组（服务端地址 / 项目主页）。
 * 信息行统一**上下布局**（标签在上、值在下）：三行风格一致，长地址独占整行完整显示，
 * 不存在「左右布局里长值被截断 / 换行」的问题。
 */
@Composable
fun AboutScreen(
    contentPadding: PaddingValues,
    onOpenUrl: (String) -> Unit,
) {
    val context = LocalContext.current
    val version = remember {
        runCatching {
            context.packageManager.getPackageInfo(context.packageName, 0).versionName
        }.getOrNull() ?: "—"
    }
    // 直读不 remember：进页即取当前生效地址（通道一小时续期/失败切换后重进即新值）
    val server = runCatching { Channel.base() }.getOrDefault("—")
    // 连接方式标签：三段降级的当前档位
    val kindLabel = when (Channel.kindValue()) {
        Channel.Kind.UGLINK -> "绿联中转"
        Channel.Kind.BACKUP -> "备用地址"
        Channel.Kind.BUILTIN -> "内置地址"
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(contentPadding)
            .padding(horizontal = 16.dp)
    ) {
        // 品牌区：logo + 名称 + slogan + 版本，居中（微信「关于微信」同构）
        Column(
            Modifier
                .fillMaxWidth()
                .padding(top = 36.dp, bottom = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Image(
                painter = painterResource(R.drawable.brand_icon),
                contentDescription = "love-life",
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .size(84.dp)
                    .clip(brandTileShape(84.dp)),
            )
            Text(
                "love-life",
                style = MaterialTheme.typography.titleLarge,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.padding(top = 14.dp),
            )
            Text(
                "只属于我们的私密生活空间",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 6.dp),
            )
        }

        // 信息组：统一上下布局
        Column(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(8.dp))
                .background(MaterialTheme.colorScheme.surface)
        ) {
            // 版本号 / 服务端地址 / 连接方式 / 项目主页
            InfoBlock("版本号", version)
            AppRowDivider(startIndent = 16.dp)
            InfoBlock("服务端地址", server)
            AppRowDivider(startIndent = 16.dp)
            InfoBlock("连接方式", kindLabel)
            AppRowDivider(startIndent = 16.dp)
            InfoBlock(
                "项目主页",
                PROJECT_URL.removePrefix("https://"),
                onClick = { onOpenUrl(PROJECT_URL) },
            )
        }
    }
}

/**
 * 信息块（上下布局）：小字标签在上，完整值在下。
 * 值不做截断——地址 / 链接截断就失去意义；独占整行后 36 字符的链接也一行放下。
 */
@Composable
private fun InfoBlock(label: String, value: String, onClick: (() -> Unit)? = null) {
    Column(
        Modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable { onClick() } else Modifier)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

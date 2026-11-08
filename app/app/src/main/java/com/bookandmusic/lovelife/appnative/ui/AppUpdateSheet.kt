package com.bookandmusic.lovelife.appnative.ui

import android.Manifest
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.AppUpdate
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppProgressBar
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSheet
import com.bookandmusic.lovelife.appnative.update.UpdateController
import com.bookandmusic.lovelife.appnative.update.UpdateStage
import kotlinx.coroutines.launch

/**
 * 应用内更新弹窗：按状态机渲染
 * 发现新版 / 下载中 / 就绪安装 / 失败 / 交接五态。
 *
 * 常驻单例（主壳与登录页各挂一次），强更态无「稍后」且蒙层不可关。
 */
@Composable
fun AppUpdateSheet() {
    val context = LocalContext.current
    val state by UpdateController.state.collectAsState()
    val scope = rememberCoroutineScope()
    // 通知权限：被拒不阻塞下载（前台服务照常保活、弹窗内仍有进度）
    val notifPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { }

    val visible = when (state.stage) {
        UpdateStage.Prompt, UpdateStage.Ready, UpdateStage.Failed, UpdateStage.Handoff -> true
        UpdateStage.Downloading -> !state.sheetCollapsed
        else -> false
    }

    val title = when (state.stage) {
        UpdateStage.Prompt -> "发现新版本 v${state.release?.versionName ?: ""}"
        UpdateStage.Downloading -> "正在下载更新"
        UpdateStage.Ready -> "更新已就绪"
        UpdateStage.Failed -> "更新失败"
        UpdateStage.Handoff -> "正在打开安装器"
        else -> ""
    }
    val confirmText = when (state.stage) {
        UpdateStage.Prompt -> "立即更新"
        UpdateStage.Downloading -> "取消下载"
        UpdateStage.Ready -> "立即安装"
        UpdateStage.Failed -> "重试"
        UpdateStage.Handoff -> "好"
        else -> ""
    }
    val cancelText = if (state.stage == UpdateStage.Downloading) "后台下载" else "稍后"

    AppSheet(
        visible = visible,
        onDismiss = { onCancel(context) },
        title = title,
        confirmText = confirmText,
        cancelText = cancelText,
        confirmDanger = state.stage == UpdateStage.Downloading,
        // 强更：不可关闭、无「稍后」（下载中除外——下载总要能取消，且此时关的只是弹窗不是流程）
        hideCancel = state.force && state.stage != UpdateStage.Downloading,
        closable = !state.force || state.stage == UpdateStage.Downloading,
        onConfirm = {
            when (state.stage) {
                UpdateStage.Prompt -> {
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                        notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
                    }
                    UpdateController.startDownload(context)
                }
                UpdateStage.Downloading -> UpdateController.cancelDownload()
                UpdateStage.Ready -> UpdateController.install(context)
                UpdateStage.Failed -> scope.launch { UpdateController.retry(context) }
                UpdateStage.Handoff -> UpdateController.closeSheet()
                else -> Unit
            }
        },
    ) {
        when (state.stage) {
            UpdateStage.Prompt -> {
                val notes = state.release?.notes.orEmpty()
                if (notes.isNotEmpty()) {
                    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        notes.forEach { line ->
                            Text(
                                line,
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurface,
                            )
                        }
                    }
                }
                val size = AppUpdate.humanBytes(state.release?.sizeBytes ?: 0)
                if (size != "—") {
                    Text(
                        "安装包大小 $size",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }

            UpdateStage.Downloading -> {
                AppProgressBar(percent = state.percent, modifier = Modifier.fillMaxWidth())
                Text(
                    "${state.percent}%",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    textAlign = TextAlign.Center,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 8.dp),
                )
                Muted("可关闭此弹窗，下载在后台继续，完成后会通知提醒")
            }

            UpdateStage.Ready -> {
                Notice("下载完成并通过校验，点击交给系统安装器安装。")
                if (state.needsPermission) {
                    Muted("需要「安装未知应用」授权，请允许后返回。")
                }
            }

            UpdateStage.Failed -> {
                Notice(state.error)
                Muted("中断的下载会自动续传。")
            }

            UpdateStage.Handoff -> Notice("请在系统安装器中确认安装，完成后应用会重启为新版本。")

            else -> Unit
        }
    }
}

@Composable
private fun Notice(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurface,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun Muted(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp),
    )
}

/** 取消/关闭：按状态分流（下载中 = 后台继续；失败与交接 = 回 idle 不写记忆） */
private fun onCancel(context: android.content.Context) {
    when (UpdateController.state.value.stage) {
        UpdateStage.Downloading -> UpdateController.collapseSheet()
        UpdateStage.Failed, UpdateStage.Handoff -> UpdateController.closeSheet()
        else -> UpdateController.dismiss(context)
    }
}

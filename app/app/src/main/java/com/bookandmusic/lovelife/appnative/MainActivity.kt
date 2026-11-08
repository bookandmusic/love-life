package com.bookandmusic.lovelife.appnative

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import com.bookandmusic.lovelife.appnative.data.AppThemeMode
import com.bookandmusic.lovelife.appnative.data.ThemePreferences
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import com.bookandmusic.lovelife.appnative.data.ApiClient
import com.bookandmusic.lovelife.appnative.ui.AppUpdateSheet
import com.bookandmusic.lovelife.appnative.update.UpdateController
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.data.SecureStore
import com.bookandmusic.lovelife.appnative.designsystem.theme.AppTheme
import com.bookandmusic.lovelife.appnative.ui.BootScreen
import com.bookandmusic.lovelife.appnative.ui.DemoApp
import com.bookandmusic.lovelife.appnative.ui.LoginScreen
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch

private enum class AppPhase { Boot, Login, Main }

class MainActivity : ComponentActivity() {
    /** 从「安装未知应用」授权页返回时自动重试安装交接 */
    override fun onResume() {
        super.onResume()
        UpdateController.onActivityResumed(this)
        // 回到前台重新选路：后台改了通道权重/加了地址，不杀进程也要生效（60s 去抖）
        Channel.refreshOnForeground()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        SecureStore.init(applicationContext)
        enableEdgeToEdge()
        // 通道前台监听：切网 / 回前台时重新探活择优（仅前台，见 Channel 契约）
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                Channel.watcherLoop()
            }
        }
        setContent {
            val context = LocalContext.current
            // 外观偏好：本地存储（个人设备偏好，不跨设备同步）
            var themeMode by remember { mutableStateOf(ThemePreferences.mode(context)) }
            var accent by remember { mutableStateOf(ThemePreferences.accent(context)) }
            val systemDark = isSystemInDarkTheme()
            val dark = when (themeMode) {
                AppThemeMode.System -> systemDark
                AppThemeMode.Light -> false
                AppThemeMode.Dark -> true
            }
            var phase by rememberSaveable { mutableStateOf(AppPhase.Boot) }
            // 任何接口 401 且刷新失败 → 回登录页：token 被清掉后不切页，用户只能手动退出或重启
            LaunchedEffect(Unit) {
                ApiClient.sessionExpired.collect { expired ->
                    if (expired) phase = AppPhase.Login
                }
            }
            // 品牌标题字体（霞鹜文楷 Lite，SIL OFL）：只用于顶栏与区块标题
            val titleFont = androidx.compose.ui.text.font.FontFamily(
                androidx.compose.ui.text.font.Font(R.font.lxgw_wenkai_lite, androidx.compose.ui.text.font.FontWeight.Normal)
            )
            AppTheme(darkTheme = dark, titleFont = titleFont, accent = accent.color) {
                when (phase) {
                    AppPhase.Boot -> BootScreen(
                        onReady = { phase = AppPhase.Main },
                        onExpired = { phase = AppPhase.Login },
                    )
                    AppPhase.Login -> LoginScreen(onLoggedIn = {
                        ApiClient.markSessionRestored()
                        phase = AppPhase.Boot
                    })
                    AppPhase.Main -> DemoApp(
                        dark = dark,
                        themeMode = themeMode,
                        onThemeModeChange = {
                            themeMode = it
                            ThemePreferences.setMode(context, it)
                        },
                        accent = accent,
                        onAccentChange = {
                            accent = it
                            ThemePreferences.setAccent(context, it)
                        },
                        onLogout = {
                            ApiClient.clearAuth()
                            phase = AppPhase.Login
                        },
                    )
                }
                // 更新弹窗常驻根节点：三个阶段共用一个实例，强更态无论停在哪个页面都拦得住
                AppUpdateSheet()
            }
        }
    }
}

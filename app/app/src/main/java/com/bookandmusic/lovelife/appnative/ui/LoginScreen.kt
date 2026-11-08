package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.AuthRepository
import com.bookandmusic.lovelife.appnative.data.errText
import androidx.compose.foundation.clickable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.KeyboardType
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppButton
import com.bookandmusic.lovelife.appnative.designsystem.atoms.brandTileShape
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppCard
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppCheckRow
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFieldRow
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import kotlinx.coroutines.launch

/**
 * 登录页：品牌区 + 玻璃表单卡（静态浮层，允许玻璃）；
 * 凭据预填不自动提交；记住登录写入安全存储；错误透传服务端文案。
 */
@Composable
fun LoginScreen(onLoggedIn: () -> Unit) {
    var username by rememberSaveable { mutableStateOf("") }
    // 密码只放内存：rememberSaveable 会把明文写进 saved state，进程重建时可被读走
    var password by remember { mutableStateOf("") }
    var showPass by rememberSaveable { mutableStateOf(false) }
    var rememberLogin by rememberSaveable { mutableStateOf(true) }
    var error by rememberSaveable { mutableStateOf("") }
    var loading by rememberSaveable { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = androidx.compose.ui.platform.LocalContext.current

    LaunchedEffect(Unit) {
        AuthRepository.loadSavedCredentials()?.let { (u, p) ->
            username = u
            password = p
        }
        // 登录页也检查一次：更新接口是登录态，故仅当本地仍有可刷新凭据时才有结果
        com.bookandmusic.lovelife.appnative.update.UpdateController.check(context)
    }

    fun submit() {
        if (loading) return
        error = ""
        loading = true
        scope.launch {
            try {
                AuthRepository.login(username.trim(), password)
                if (rememberLogin) {
                    AuthRepository.rememberCredentials(username.trim(), password)
                } else {
                    AuthRepository.forgetCredentials()
                }
                onLoggedIn()
            } catch (e: Exception) {
                error = errText(e, "登录失败")
            } finally {
                loading = false
            }
        }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
    ) {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Column(Modifier.fillMaxWidth().padding(horizontal = 28.dp)) {
                    Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        androidx.compose.foundation.Image(
                            painter = androidx.compose.ui.res.painterResource(
                                com.bookandmusic.lovelife.appnative.R.drawable.brand_icon
                            ),
                            contentDescription = "情侣生活记录",
                            modifier = Modifier.size(84.dp).clip(brandTileShape(84.dp)),
                        )
                        Text(
                            "情侣生活记录",
                            style = MaterialTheme.typography.headlineSmall,
                            color = MaterialTheme.colorScheme.onSurface,
                            modifier = Modifier.padding(top = 14.dp),
                        )
                        Text(
                            "只属于我们的私密生活空间",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 2.dp),
                        )
                    }
                    Column(Modifier.fillMaxWidth().padding(top = 28.dp)) {
                        AppCard(modifier = Modifier.fillMaxWidth(), corner = 12.dp, padding = 0.dp) {
                            AppFieldRow(
                                label = "用户名",
                                value = username,
                                onValueChange = { username = it },
                                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Text),
                            )
                            AppRowDivider(startIndent = 16.dp)
                            AppFieldRow(
                                label = "密码",
                                value = password,
                                onValueChange = { password = it },
                                visualTransformation = if (showPass) {
                                    VisualTransformation.None
                                } else {
                                    PasswordVisualTransformation()
                                },
                                trailing = {
                                    Box(
                                        Modifier
                                            .size(40.dp)
                                            .clip(RoundedCornerShape(999.dp))
                                            .clickable { showPass = !showPass },
                                        contentAlignment = Alignment.Center,
                                    ) {
                                        Icon(
                                            if (showPass) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility,
                                            contentDescription = if (showPass) "隐藏密码" else "显示密码",
                                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                                            modifier = Modifier.size(18.dp),
                                        )
                                    }
                                },
                            )
                            AppRowDivider(startIndent = 16.dp)
                            AppCheckRow("记住登录", rememberLogin, onCheckedChange = { rememberLogin = it })
                        }
                        if (error.isNotEmpty()) {
                            Text(
                                error,
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.error,
                                modifier = Modifier.fillMaxWidth().padding(top = 8.dp, start = 4.dp),
                            )
                        }
                        AppButton(
                            if (loading) "正在进入…" else "进入空间",
                            onClick = { submit() },
                            enabled = !loading && username.isNotBlank() && password.isNotBlank(),
                            modifier = Modifier.padding(top = 16.dp),
                        )
                    }
                }
            }
    }
}

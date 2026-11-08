package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/** 分组内的行分隔线（0.5px）：放在两行之间。 */
@Composable
fun AppRowDivider(modifier: Modifier = Modifier, startIndent: Dp = 0.dp) {
    val colors = LocalAppColors.current
    Box(
        modifier
            .fillMaxWidth()
            .padding(start = startIndent)
            .height(0.5.dp)
            .background(colors.rule)
    )
}

/**
 * 表单行：左侧 15sp 标签 + 右侧无边框输入，聚焦时整行主色下划线 +
 * 标签变主色。用 BasicTextField 自绘，避免 OutlinedTextField 的 Material 描边框。
 */
@Composable
fun AppFieldRow(
    label: String,
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    singleLine: Boolean = true,
    placeholder: String? = null,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    trailing: (@Composable () -> Unit)? = null,
) {
    val colors = LocalAppColors.current
    var focused by remember { mutableStateOf(false) }
    Column(modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .heightIn(min = 48.dp)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                label,
                style = MaterialTheme.typography.bodyLarge,
                color = if (focused) colors.primaryDeep else MaterialTheme.colorScheme.onSurface,
            )
            Box(Modifier.weight(1f)) {
                BasicTextField(
                    value = value,
                    onValueChange = onValueChange,
                    modifier = Modifier
                        .fillMaxWidth()
                        .onFocusChanged { focused = it.isFocused },
                    singleLine = singleLine,
                    textStyle = LocalTextStyle.current.merge(
                        MaterialTheme.typography.bodyLarge.copy(color = MaterialTheme.colorScheme.onSurface)
                    ),
                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                    visualTransformation = visualTransformation,
                    keyboardOptions = keyboardOptions,
                    decorationBox = { inner ->
                        if (value.isEmpty() && placeholder != null) {
                            Text(
                                placeholder,
                                style = MaterialTheme.typography.bodyLarge,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        inner()
                    },
                )
            }
            trailing?.invoke()
        }
        Box(
            Modifier
                .fillMaxWidth()
                .height(2.dp)
                .background(if (focused) colors.primaryDeep else Color.Transparent)
        )
    }
}

/** 勾选行（登录页「记住登录」）：方框勾选，主色填充。 */
@Composable
fun AppCheckRow(
    label: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = LocalAppColors.current
    Row(
        modifier
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            // 复选框语义：角色与勾选状态交给 toggleable（行内文字合并进同一节点）；
            // 图标只是装饰，读屏状态从行节点读，不让小图标单独成为焦点
            .toggleable(value = checked, role = Role.Checkbox, onValueChange = onCheckedChange)
            .semantics { stateDescription = if (checked) "已勾选" else "未勾选" }
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
        Box(
            Modifier
                .size(20.dp)
                .clip(RoundedCornerShape(8.dp))
                .then(
                    if (checked) Modifier.background(MaterialTheme.colorScheme.primary) else Modifier
                )
                .then(
                    if (checked) {
                        Modifier
                    } else {
                        Modifier.drawBehind {
                            drawRoundRect(
                                color = colors.rule,
                                style = Stroke(width = 1.5.dp.toPx()),
                                cornerRadius = CornerRadius(8.dp.toPx()),
                            )
                        }
                    }
                ),
            contentAlignment = Alignment.Center,
        ) {
            if (checked) {
                Icon(
                    Icons.Filled.Check,
                    // 装饰图标：勾选状态由行节点的复选框语义读出
                    contentDescription = null,
                    tint = colors.onSolid,
                    modifier = Modifier.size(14.dp),
                )
            }
        }
    }
}

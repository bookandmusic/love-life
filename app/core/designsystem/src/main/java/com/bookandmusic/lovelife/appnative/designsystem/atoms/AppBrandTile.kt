package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.unit.Dp

/**
 * 品牌图圆角：按显示尺寸的 **30%** 圆角展示。
 *
 * 唯一合法来源——业务侧禁止写死数值：About 与 Login 同为 84dp，各写一个就会出现
 * 同尺寸不同圆角。改品牌图规格只改这里。
 */
fun brandTileShape(size: Dp): Shape = RoundedCornerShape(size * 0.3f)

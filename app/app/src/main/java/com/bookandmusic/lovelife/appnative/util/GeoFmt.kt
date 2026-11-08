package com.bookandmusic.lovelife.appnative.util

import java.util.Locale

/**
 * 坐标字符串：固定小数位、与系统语言无关。
 *
 * `"%.6f".format(v)` 走默认 Locale：德语/阿拉伯语环境会得到「31,230400」「٣١٫٢٣٠٤٠٠」，
 * 这种串再喂给 `toDoubleOrNull()` 或发给服务端都不合法，故统一用 [Locale.US]。
 */
fun formatCoord(value: Double, digits: Int = 6): String =
    String.format(Locale.US, "%.${digits}f", value)

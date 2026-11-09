package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.ui.Route
import com.bookandmusic.lovelife.appnative.ui.backTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * 壳内返回策略：根页（主壳 / AI 对话页）不拦截系统返回，二级页回上级，设置页由调用方收尾。
 * 回归点：AI 对话页曾被当成二级页，系统返回落到传统主壳，界面与 appMode 不一致。
 */
class ShellBackTest {

    @Test fun `rootRoutesDoNotInterceptBack`() {
        assertNull(backTarget(Route.Main()))
        assertNull(backTarget(Route.Main(focusPostId = 7L)))
        assertNull(backTarget(Route.Chat))
    }

    @Test fun `secondaryRoutesGoToParent`() {
        assertEquals(Route.Settings, backTarget(Route.About))
        assertEquals(Route.Anniversaries, backTarget(Route.AnniversaryForm))
        assertEquals(Route.Main(), backTarget(Route.AlbumDetail(3L, "婚礼")))
    }

    @Test fun `listRoutesReturnToShell`() {
        assertEquals(Route.Main(), backTarget(Route.Settings))
        assertEquals(Route.Main(), backTarget(Route.Notifications))
        assertEquals(Route.Main(), backTarget(Route.Anniversaries))
        assertEquals(Route.Main(), backTarget(Route.Publish))
        assertEquals(Route.Main(), backTarget(Route.AlbumForm))
        assertEquals(Route.Main(), backTarget(Route.PointForm))
    }
}

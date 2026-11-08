package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.MapRepository
import org.junit.Assert.assertEquals
import org.junit.Test

/** 瓦片签名换签间隔：长驻地图页靠它续命，算错就是 30 分钟后地图整片空白。 */
class MapSignatureRefreshTest {

    private val now = 1_700_000_000_000L

    @Test fun `refreshBeforeExpiry`() {
        // exp 在 10 分钟后 → 提前 2 分钟，8 分钟后再换
        assertEquals(8 * 60_000L, MapRepository.refreshDelayMs((now / 1000) + 600, now))
    }

    @Test fun `clampUpperBound`() {
        // exp 在 60 分钟后（异常长）→ 夹到 25 分钟，不至于长驻页面永不换签
        assertEquals(25 * 60_000L, MapRepository.refreshDelayMs((now / 1000) + 3600, now))
    }

    @Test fun `clampLowerBoundWhenExpiring`() {
        // 已过期 / 即将过期 → 不空转，至少等 1 分钟再取
        assertEquals(60_000L, MapRepository.refreshDelayMs((now / 1000) - 600, now))
        assertEquals(60_000L, MapRepository.refreshDelayMs((now / 1000) + 60, now))
    }

    @Test fun `fallbackWhenExpMissing`() {
        // 服务端没下发 exp → 兜底 20 分钟
        assertEquals(20 * 60_000L, MapRepository.refreshDelayMs(0, now))
    }
}

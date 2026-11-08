package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.update.UpdateController
import com.bookandmusic.lovelife.appnative.update.UpdateController.Decision
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 检查更新的决策表（`UpdateController.decide`）。
 *
 * 顺序即语义：真机上报过一次「下载完点稍后，每次启动还弹」——原因是冷启动恢复排在
 * 「稍后」记忆之前，只要缓存里有成品就永远走 Ready。这张表把顺序钉死。
 */
class UpdateDecisionTest {

    /** 便捷封装：默认「有新版本、非强更、未记过稍后」 */
    private fun decide(
        hasCached: Boolean = false,
        force: Boolean = false,
        manual: Boolean = false,
        dismissed: Int = 0,
        releaseCode: Int = 2001,
        available: Boolean = true,
    ) = UpdateController.decide(
        isUpdateAvailable = available,
        hasCachedPackage = hasCached,
        force = force,
        manual = manual,
        dismissedCode = dismissed,
        releaseCode = releaseCode,
    )

    @Test
    fun noNewVersionStaysIdle() {
        assertEquals(Decision.Idle, decide(available = false, hasCached = true))
    }

    @Test
    fun firstLaunchWithUpdatePrompts() {
        assertEquals(Decision.Prompt, decide())
    }

    @Test
    fun downloadedPackageGoesStraightToReady() {
        // 冷启动恢复：成品已在缓存 → 不重弹确认，直接待安装
        assertEquals(Decision.Ready, decide(hasCached = true))
    }

    // 真机 bug 回归：下载完 → 点「稍后」→ 杀进程重开，不得再弹。
    @Test
    fun dismissedAfterDownloadDoesNotPromptOnRestart() {
        assertEquals(
            Decision.Idle,
            decide(hasCached = true, dismissed = 2001, releaseCode = 2001),
        )
    }

    @Test
    fun dismissedWithoutCachedPackageAlsoStaysQuiet() {
        assertEquals(Decision.Idle, decide(hasCached = false, dismissed = 2001, releaseCode = 2001))
    }

    @Test
    fun manualCheckBypassesDismissMemory() {
        // 手动检查：用户主动要看，记过稍后也要给结果（有包 → 直接待安装）
        assertEquals(Decision.Ready, decide(hasCached = true, dismissed = 2001, manual = true))
        assertEquals(Decision.Prompt, decide(hasCached = false, dismissed = 2001, manual = true))
    }

    @Test
    fun forceUpdateIgnoresDismissMemory() {
        assertEquals(
            Decision.Prompt,
            decide(hasCached = false, force = true, dismissed = 2001, releaseCode = 2001),
        )
        // 强更且包已下好：直接推进到待安装，不给「稍后」
        assertEquals(Decision.Ready, decide(hasCached = true, force = true, dismissed = 2001))
    }

    @Test
    fun dismissMemoryIsPerVersion() {
        // 记忆的是别的版本 → 新版本仍要提示
        assertEquals(Decision.Prompt, decide(hasCached = false, dismissed = 2000, releaseCode = 2001))
    }
}
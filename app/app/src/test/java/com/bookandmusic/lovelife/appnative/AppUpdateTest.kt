package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.AppUpdate
import com.bookandmusic.lovelife.appnative.data.UpdateLatestDto
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** 应用内更新的纯逻辑：清单解析 / 版本比较 / 强更判定 / 体积文案。 */
class AppUpdateTest {

    private fun dto(
        available: Boolean = true,
        versionName: String = "0.2.0",
        versionCode: Int = 2000,
        notes: String = "",
        minVersionCode: Int = 0,
        assetName: String = "love-life-0.2.0-app-universal-release.apk",
        assetSize: Long = 49_109_825,
        downloadPath: String = "/api/v1/app/update/download",
    ) = UpdateLatestDto(
        available = available,
        versionName = versionName,
        versionCode = versionCode,
        notes = notes,
        minVersionCode = minVersionCode,
        assetName = assetName,
        assetSize = assetSize,
        downloadPath = downloadPath,
    )

    @Test
    fun unavailableMeansNoUpdateInsteadOfError() {
        assertNull(AppUpdate.releaseOf(UpdateLatestDto(available = false, reason = "服务端暂无已就绪的安装包")))
    }

    @Test
    fun missingVersionInfoIsRejected() {
        val noVersion = runCatching { AppUpdate.releaseOf(dto(versionName = "", versionCode = 0)) }
        assertTrue(noVersion.isFailure)
        val noAsset = runCatching { AppUpdate.releaseOf(dto(assetName = "", downloadPath = "")) }
        assertTrue(noAsset.isFailure)
    }

    @Test
    fun releaseCarriesDownloadTargetAndSize() {
        val r = AppUpdate.releaseOf(dto())!!
        assertEquals("0.2.0", r.versionName)
        assertEquals(2000, r.versionCode)
        assertEquals("love-life-0.2.0-app-universal-release.apk", r.fileName)
        assertEquals("/api/v1/app/update/download", r.downloadPath)
        assertEquals(49_109_825L, r.sizeBytes)
    }

    // 「最新 > 本机」是更新提示的唯一判据：装机 1003（旧包）遇到 2000（新版）必须为真。
    @Test
    fun newerReleaseCodeWins() {
        assertTrue(AppUpdate.isUpdateAvailable(localCode = 1003, latestCode = 2000))
        assertFalse(AppUpdate.isUpdateAvailable(localCode = 2000, latestCode = 2000))
        assertFalse(AppUpdate.isUpdateAvailable(localCode = 2001, latestCode = 2000))
    }

    @Test
    fun forceUpdateOnlyWhenBelowMinimum() {
        assertFalse(AppUpdate.isForceUpdate(localCode = 1003, minVersionCode = 0))
        assertTrue(AppUpdate.isForceUpdate(localCode = 1003, minVersionCode = 2000))
        assertFalse(AppUpdate.isForceUpdate(localCode = 2000, minVersionCode = 2000))
    }

    @Test
    fun notesDropMinVersionLineAndBlankLines() {
        val notes = AppUpdate.noteLines("### 更新内容\n- 修了某个问题\n\nmin-version-code: 2000\n")
        assertEquals(listOf("### 更新内容", "- 修了某个问题"), notes)
    }

    @Test
    fun humanBytesReadsAsFileSize() {
        assertEquals("—", AppUpdate.humanBytes(0))
        assertEquals("900 B", AppUpdate.humanBytes(900))
        assertEquals("2 KB", AppUpdate.humanBytes(2048))
        assertEquals("46.8 MB", AppUpdate.humanBytes(49_109_825))
    }

    /**
     * 版本量纲守卫：versionCode 必须由 versionName 按 major*1000000 + minor*1000 + patch 推导，
     * 手写常量会让客户端的「最新 > 本机」判定失效（服务端按同一公式下发）。
     */
    @Test
    fun versionCodeFollowsVersionName() {
        val parts = BuildConfig.VERSION_NAME.split(".")
        assertEquals(3, parts.size)
        val expected = parts[0].toInt() * 1_000_000 + parts[1].toInt() * 1_000 + parts[2].toInt()
        assertEquals(expected, BuildConfig.VERSION_CODE)
    }
}

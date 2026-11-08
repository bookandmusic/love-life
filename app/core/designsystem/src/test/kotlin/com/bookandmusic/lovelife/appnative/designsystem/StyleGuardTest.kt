package com.bookandmusic.lovelife.appnative.designsystem

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 风格守卫（视觉令牌的机器可执行部分）：任一规则失败 = 构建不通过。
 * 对应 admin 端 guard-styles 的定位：拦截 Agent 在业务层的风格漂移。
 */
class StyleGuardTest {

    private val root: File = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
        .first { File(it, "settings.gradle.kts").exists() }

    private fun sources(dir: String): List<File> =
        File(root, dir).walkTopDown().filter { it.isFile && it.extension == "kt" }.toList()

    private fun appSources(): List<File> = sources("app/src/main")

    @Test
    fun `appRejectsRefractionGlass`() {
        val offenders = appSources().filter {
            it.readText().contains("import io.github.itshedimisawi.vitro") ||
                it.readText().contains("appGlass(") ||
                it.readText().contains("AppGlassHost")
        }
        assertTrue("chrome 材质只能经 designsystem 的 appChrome()，越界文件：$offenders", offenders.isEmpty())
    }

    @Test
    fun `appRejectsMaterial3Interactive`() {
        val banned = Regex(
            "^import androidx\\.compose\\.material3\\.(Button|ButtonDefaults|Card|CardDefaults|TextField|OutlinedTextField|Scaffold|TopAppBar|FloatingActionButton|ExtendedFloatingActionButton|ModalBottomSheet|Switch|SwitchDefaults|Slider|Checkbox|RadioButton|SwipeToDismissBox)",
        )
        val offenders = appSources()
            .flatMap { it.readText().lines().filter { line -> banned.containsMatchIn(line.trim()) }.map { line -> it.name to line } }
        assertTrue("交互组件必须来自 designsystem 的 App* 封装，越界：$offenders", offenders.isEmpty())
    }

    @Test
    fun `appRejectsHardcodedColors`() {
        val offenders = appSources().filter { it.readText().contains(Regex("Color\\(0x")) }
        assertTrue("颜色只能经 MaterialTheme token（色板在 designsystem/AppPalette），越界文件：$offenders", offenders.isEmpty())
    }

    @Test
    fun `appRejectsFab`() {
        val offenders = appSources().filter { it.readText().contains("FloatingActionButton") }
        assertTrue("本 app 禁用 FAB：添加入口用导航条角色按钮或页内场景化按钮，越界文件：$offenders", offenders.isEmpty())
    }

    @Test
    fun `appRejectsHardcodedFontSize`() {
        val offenders = appSources().filter { it.readText().contains(Regex("\\d+\\.sp")) }
        assertTrue("字号只能用 Typography 档位（17/15/13/11），越界文件：$offenders", offenders.isEmpty())
    }

    @Test
    fun `appRejectsAmbientGradient`() {
        val offenders = appSources().filter {
            it.readText().contains("LocalApp" + "Atmosphere")
        }
        assertTrue("画布是 iOS 中性色（#F2F2F7 / #000000），禁止全屏氛围渐变，越界文件：$offenders", offenders.isEmpty())
    }

    @Test
    fun `appContentPanelsRejectGlass`() {
        val offenders = appSources()
            .flatMap { file ->
                file.readText().lines()
                    .filter { it.contains("appGlass(") && !it.trimStart().startsWith("*") }
                    .map { line -> file.name to line.trim() }
            }
            // 允许：顶栏 / 导航条 / 地图缩放控件（chrome）
            .filterNot { (name, line) ->
                name == "MapScreen.kt" || name == "DemoApp.kt"
            }
        assertTrue("玻璃只属于 chrome（顶栏/导航条/弹层/地图控件），内容卡一律实色：$offenders", offenders.isEmpty())
    }

    @Test
    fun `imageResourcesFollowManualDarkSwitch`() {
        // 唯一豁免：MainActivity 解析「跟随系统」档位时必须读系统设置；
        // 其余任何页面/组件都不得直接用 isSystemInDarkTheme（会用错判据导致图片不跟随切换）。
        val allowed = setOf("MainActivity.kt")
        val offenders = appSources().filter {
            it.name !in allowed && it.readText().contains("isSystemInDarkTheme")
        }
        assertTrue(
            "深浅由用户手动切换，页面必须用 LocalAppDark.current：$offenders",
            offenders.isEmpty(),
        )
    }

    @Test
    fun `cardCornersOnlyFourTiers`() {
        val allowed = setOf("8", "12", "20", "999")
        val offenders = appSources().flatMap { file ->
            Regex("corner = (\\d+)\\.dp").findAll(file.readText())
                .map { it.groupValues[1] }
                .filterNot { it in allowed }
                .map { file.name to it }
        }
        assertTrue("圆角只允许 8 / 12 / 20 / 999，越界：$offenders", offenders.isEmpty())
    }

    @Test
    fun `directRoundedCornerShapeOnlyFourTiers`() {
        // `corner = N.dp` 只覆盖 AppCard 参数，.clip(RoundedCornerShape(N.dp)) 直调
        // 拦不到——直调写法只有这条规则能拦，档外圆角会在构建期暴露。
        // 豁免 2dp：细线与细条（高 2dp 的线/分隔条），视觉等同直角，不入档位。
        val allowed = setOf("2", "8", "12", "20", "999")
        val offenders = appSources().flatMap { file ->
            Regex("""RoundedCornerShape\((\d+)\.dp\)""").findAll(file.readText())
                .map { it.groupValues[1] }
                .filterNot { it in allowed }
                .map { file.name to it }
        }
        assertTrue("RoundedCornerShape 直调只允许 2 / 8 / 12 / 20 / 999（2 仅媒体格缝），越界：$offenders", offenders.isEmpty())
    }

    @Test
    fun `brandIconCornersViaBrandTileShape`() {
        // Boot/Login/About 三页的品牌图圆角必须走 brandTileShape(size)：同为 84dp 的页面取值一致。
        // 判据：引用 R.drawable.brand_icon 的语句 3 行内出现 .clip(RoundedCornerShape(N.dp))
        // 字面量（合法的 999 胶囊/8 小元素不与品牌图同节点，不会命中）。
        val brandPages = appSources().filter {
            it.name in setOf("BootScreen.kt", "LoginScreen.kt", "AboutScreen.kt")
        }
        val offenders = brandPages.flatMap { file ->
            val lines = file.readText().lines()
            lines.withIndex().filter { it.value.contains("brand_icon") }.flatMap { (idx, _) ->
                lines.subList(idx, minOf(idx + 3, lines.size))
                    .filter { Regex("""RoundedCornerShape\((\d+)\.dp\)""").containsMatchIn(it) }
                    .map { file.name to it.trim() }
            }
        }
        assertTrue("品牌图圆角必须用 brandTileShape(size)，禁止写死数值：$offenders", offenders.isEmpty())
    }

    @Test
    fun `cardsAlwaysHaveShadow`() {
        val offenders = appSources().filter { it.readText().contains("shadow = false") }
        assertTrue("所有内容卡统一带阴影，禁止 shadow = false：$offenders", offenders.isEmpty())
    }

    @Test
    fun `scrollContainersClearBottomNav`() {
        // 判据：页面级 LazyColumn / verticalScroll 必须出现 contentPadding 或 calculateBottomPadding，
        // 否则最后一项会被悬浮导航条压住。
        val pageScreens = appSources().filter {
            it.name in setOf(
                "UsScreen.kt", "TimelineScreen.kt", "MapScreen.kt",
                "FormScreens.kt", "SettingsScreen.kt", "DemoScreens.kt",
            )
        }
        val offenders = pageScreens.filter { file ->
            val text = file.readText()
            val hasScroll = text.contains("LazyColumn") || text.contains("verticalScroll")
            // 三种合法写法：LazyColumn 的 contentPadding=、显式 calculateBottomPadding、或整页 .padding(contentPadding)
            val hasBottom = text.contains("calculateBottomPadding") ||
                text.contains("contentPadding = contentPadding") ||
                text.contains(".padding(contentPadding)")
            hasScroll && !hasBottom
        }
        assertTrue("整页滚动容器必须让开底部导航条：$offenders", offenders.isEmpty())
    }

    @Test
    fun `deleteOnlyViaLongPress`() {
        val offenders = appSources().filter {
            it.readText().contains("AppSwipeItem") || it.readText().contains("SwipeToDismiss")
        }
        assertTrue("删除入口只有长按 + AppActionSheet，禁止左滑：$offenders", offenders.isEmpty())
    }

    @Test
    fun `chromeSurfacesNearOpaque`() {
        val chrome = File(
            root,
            "core/designsystem/src/main/java/com/bookandmusic/lovelife/appnative/designsystem/atoms/AppChrome.kt",
        )
        assertTrue("chrome 表面文件必须存在", chrome.exists())
        val text = chrome.readText()
        assertTrue(
            "Bar/Floating/Overlay 三档都必须 >= 0.95 alpha，否则下方内容会透上来",
            text.contains("0.97f") && text.contains("0.95f") && text.contains("1f"),
        )
    }

    @Test
    fun `chromeConsumesTouch`() {
        val navBar = File(root, "core/designsystem/src/main/java/com/bookandmusic/lovelife/appnative/designsystem/atoms/AppNavBar.kt").readText()
        val topBar = File(root, "core/designsystem/src/main/java/com/bookandmusic/lovelife/appnative/designsystem/atoms/AppTopBar.kt").readText()
        assertTrue("AppNavBar 必须 consumeTouches()", navBar.contains("consumeTouches()"))
        assertTrue("AppTopBar 必须 consumeTouches()", topBar.contains("consumeTouches()"))
    }

}

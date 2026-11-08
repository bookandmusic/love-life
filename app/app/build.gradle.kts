import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// 发布号唯一源是 git tag：CI 把 tag 当 -PAPP_VERSION_NAME 传进来，出包后回读产物里的版本核对。
// 下面这个默认值只是**本地/模拟器构建的兜底号**，允许落后于最新 tag——它不决定发布版本，别为发版改它。
// -PAPP_VERSION_NAME 的另一用途：本地刻意造「覆盖安装升级」场景时临时指定版本号。
val appVersionName: String =
    (findProperty("APP_VERSION_NAME") as String?)?.trim()?.takeIf { it.isNotEmpty() } ?: "0.2.0"

// versionCode 由版本号推导，公式与 APK 内写死的值、服务端下发的 version_code 必须同量纲：
// major*1000000 + minor*1000 + patch（服务端 versionCode 落在同一公式上）。
val appVersionCode: Int = run {
    val parts = appVersionName.split(".")
    require(parts.size >= 3) { "versionName 必须是 x.y.z：$appVersionName" }
    val nums = parts.take(3).map {
        it.trim().toIntOrNull() ?: error("versionName 段不是数字：$appVersionName")
    }
    nums[0] * 1_000_000 + nums[1] * 1_000 + nums[2]
}

// 签名配置（仅 release）：keystore.properties 由 `make android-sign-env` 生成，不入库。
// 无该文件时不禁用 debug 构建，只在真要出 release 时失败（见下方 taskGraph 校验）。
val keystorePropsFile = rootProject.file("keystore.properties")
val keystoreProps = Properties().apply {
    if (keystorePropsFile.exists()) keystorePropsFile.inputStream().use { load(it) }
}
val hasKeystore = keystorePropsFile.exists()

// 内置服务地址：构建期注入。
// 环境变量 APP_DEFAULT_BASE 优先，其次 -PAPP_DEFAULT_BASE=...；仓库内不写死任何地址。
// 本地与 CI 同一条路：指到哪台后端，就编出连哪台后端的包。
val appDefaultBase: String =
    (System.getenv("APP_DEFAULT_BASE") ?: (findProperty("APP_DEFAULT_BASE") as String?) ?: "").trim()

android {
    namespace = "com.bookandmusic.lovelife.appnative"
    compileSdk = 37
    defaultConfig {
        applicationId = "com.bookandmusic.lovelife.appnative"
        minSdk = 26
        targetSdk = 36
        versionCode = appVersionCode
        versionName = appVersionName
        buildConfigField("String", "DEFAULT_BASE", "\"$appDefaultBase\"")
    }
    signingConfigs {
        create("release") {
            // 赋值必须在 hasKeystore 之内：否则本地没密钥时配置阶段的空值强制转换会让 debug 也失败。
            if (hasKeystore) {
                keyAlias = keystoreProps["keyAlias"] as String
                keyPassword = keystoreProps["password"] as String
                storeFile = file(keystoreProps["storeFile"] as String)
                storePassword = keystoreProps["password"] as String
            }
        }
    }
    buildTypes {
        getByName("release") {
            if (hasKeystore) signingConfig = signingConfigs.getByName("release")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_25
        targetCompatibility = JavaVersion.VERSION_25
    }
    buildFeatures { compose = true; buildConfig = true }
}

// 语言级别不等于工具链：声明 JDK 25，构建自行定位（装在哪都行；找不到 25 时报错而非换版本），
// 找不到才报错——不依赖「机器 JAVA_HOME 恰好是 17」。
kotlin {
    jvmToolchain(25)
}

// 出包的两条硬前提，缺一不许产出 APK：
//   1) 内置服务地址——没有地址的包装上去连不上任何服务端，登录只报「登录失败」，看不出原因；
//   2) release 变体还要签名密钥——没有密钥装不上。
// 判定放在 taskGraph 就绪时、且只看「打包」任务：编译与单测不依赖部署事实，
// 没有地址的机器/CI 照样能跑测试（只是产不出包）。
gradle.taskGraph.whenReady {
    // 只认「产出 APK/AAB」的任务名：packageDebugResources 之类是单测也要用的中间产物，不算出包
    val packagingNames = setOf(
        "assembleDebug", "assembleRelease", "bundleDebug", "bundleRelease",
        "packageDebug", "packageRelease",
    )
    val packaging = allTasks.filter { it.name in packagingNames }
    if (packaging.isEmpty()) return@whenReady
    val wantsRelease = packaging.any { it.name.contains("Release") }
    if (appDefaultBase.isEmpty()) {
        throw GradleException(
            "出包需要内置服务地址：请设置环境变量 APP_DEFAULT_BASE" +
                "（如 APP_DEFAULT_BASE=http://192.168.1.10:8080），或传 -PAPP_DEFAULT_BASE=..."
        )
    }
    if (wantsRelease && !hasKeystore) {
        throw GradleException(
            "release 构建需要签名密钥：先生成 app/keystore.properties（make android-sign-env）"
        )
    }
}

dependencies {
    implementation(project(":core:designsystem"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime)
    implementation(libs.okhttp)
    implementation(libs.androidx.security.crypto)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.coil.compose)
    implementation(libs.media3.exoplayer)
    implementation(libs.media3.ui)
    testImplementation(libs.junit4)
}

// 测试报告文件名带非 ASCII 时，非 UTF-8 locale 的机器写不出 HTML（失败报告恰好是最需要看的时候）：
// 显式给测试 JVM 指定 UTF-8 文件编码作兜底（测试方法名已尽量用 ASCII）。
tasks.withType<Test>().configureEach {
    jvmArgs("-Dfile.encoding=UTF-8")
}

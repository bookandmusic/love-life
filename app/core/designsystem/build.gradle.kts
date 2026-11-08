plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "com.bookandmusic.lovelife.appnative.designsystem"
    compileSdk = 37
    defaultConfig { minSdk = 26 }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_25
        targetCompatibility = JavaVersion.VERSION_25
    }
    buildFeatures { compose = true }
}

// 与 :app 同一工具链口径：JDK 25，由构建自行定位（不依赖机器默认 JDK）
kotlin {
    jvmToolchain(25)
}

dependencies {
    api(platform(libs.androidx.compose.bom))
    api(libs.androidx.compose.ui)
    api(libs.androidx.compose.foundation)
    api(libs.androidx.compose.material3)
    api(libs.androidx.compose.icons.core)
    api(libs.androidx.compose.icons.extended)
    testImplementation(libs.junit4)
    implementation(libs.androidx.activity.compose)
    api(libs.coil.compose)
}

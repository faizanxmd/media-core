import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// build.sh passes these; a plain `gradle assembleRelease` gets a dev build
val vCode = (findProperty("vcode") as String?)?.toInt() ?: 1
val vName = (findProperty("vname") as String?) ?: "dev"

android {
    namespace = "app.mediacore"
    compileSdk = 36

    defaultConfig {
        applicationId = "app.mediacore"
        minSdk = 26
        targetSdk = 36
        versionCode = vCode
        versionName = vName
        // TVs and phones are ARM; the FFmpeg audio decoder ships native code per ABI
        ndk { abiFilters += ((findProperty("abis") as String?) ?: "arm64-v8a,armeabi-v7a").split(",") }
    }

    // The TV app. The phone app is its own module, ../phone
    flavorDimensions += "device"
    productFlavors {
        create("tv") { dimension = "device" }
    }

    signingConfigs {
        create("release") {
            storeFile = file(System.getenv("MEDIA_CORE_KEYSTORE") ?: "${System.getProperty("user.home")}/.config/media-core/app.jks")
            storePassword = System.getenv("MEDIA_CORE_KS_PASS")
            keyAlias = "media-core"
            keyPassword = System.getenv("MEDIA_CORE_KS_PASS")
        }
    }
    buildTypes {
        release {
            // R8: smaller, faster code (Compose runs noticeably slower unoptimised on a weak TV)
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
            // test builds only: lets adb run-as push test media into the app
            isDebuggable = findProperty("debuggable") == "true"
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    lint { checkReleaseBuilds = false }
}

kotlin { compilerOptions { jvmTarget.set(JvmTarget.JVM_17) } }

dependencies {
    implementation(project(":core"))
    val media3 = "1.8.0"
    implementation(platform("androidx.compose:compose-bom:2025.09.00"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.media3:media3-exoplayer:$media3")
    implementation("androidx.media3:media3-ui:$media3")
    implementation("androidx.media3:media3-datasource-okhttp:$media3")
    // Jellyfin's build of Media3's FFmpeg extension: decodes DTS, TrueHD, etc.
    // when the TV can't (TV speakers only, no receiver)
    implementation("org.jellyfin.media3:media3-ffmpeg-decoder:$media3+1")
    implementation("io.coil-kt.coil3:coil-compose:3.3.0")
    implementation("io.coil-kt.coil3:coil-network-okhttp:3.3.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.google.zxing:core:3.5.4") // the pairing screen's QR code
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
}

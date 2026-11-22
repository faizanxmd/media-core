import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// build.sh passes these; a plain `gradle :phone:assembleRelease` gets a dev build
val vCode = (findProperty("vcode") as String?)?.toInt() ?: 1
val vName = (findProperty("vname") as String?) ?: "dev"

// The phone app: its screens and player are its own. Plumbing it shares with the TV app is in ../core.
android {
    namespace = "app.mediacore.phone"
    compileSdk = 36

    defaultConfig {
        applicationId = "app.mediacore.phone"
        minSdk = 26
        targetSdk = 36
        versionCode = vCode
        versionName = vName
        // the FFmpeg audio decoder ships native code per ABI
        ndk { abiFilters += ((findProperty("abis") as String?) ?: "arm64-v8a").split(",") }
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
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
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
    // Jellyfin's build of Media3's FFmpeg extension: decodes DTS, TrueHD, etc. when the phone can't
    implementation("org.jellyfin.media3:media3-ffmpeg-decoder:$media3+1")
    implementation("io.coil-kt.coil3:coil-compose:3.3.0")
    implementation("io.coil-kt.coil3:coil-network-okhttp:3.3.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.11.0")
}

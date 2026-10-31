pluginManagement {
    repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories { google(); mavenCentral() }
}
rootProject.name = "media-core"
include(":app") // the TV app
include(":phone") // the phone app: its screens and player are its own
include(":core") // what both apps share: engine API, data, offline copies, scrub pictures

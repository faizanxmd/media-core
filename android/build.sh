#!/bin/bash
# Build the TV app and publish it where the engine serves it (the app updates
# itself from /api/v1/app.apk), or build the separate phone app.
# Usage: android/build.sh [--install <tv-ip>] | android/build.sh --phone
#   --install: also push it to a device over adb (TV: Developer options > Network debugging)
#   --phone:   build the phone app instead -> $OUT/media-core-phone.apk
# Env (all optional): ANDROID_HOME, JAVA_HOME, GRADLE, MEDIA_CORE_APK_DIR
set -euo pipefail
cd "$(dirname "$0")"

# Android SDK: $ANDROID_HOME, else the usual Linux / macOS locations
if [ -z "${ANDROID_HOME:-}" ]; then
  for d in "$HOME/android/sdk" "$HOME/Android/Sdk" "$HOME/Library/Android/sdk"; do
    [ -d "$d" ] && ANDROID_HOME="$d" && break
  done
fi
[ -n "${ANDROID_HOME:-}" ] || { echo "no Android SDK: set ANDROID_HOME or run scripts/android-setup.sh" >&2; exit 1; }
export ANDROID_HOME
# JDK 17
if [ -z "${JAVA_HOME:-}" ] && [ -d /usr/lib/jvm/java-17-openjdk-amd64 ]; then
  export JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64
fi
# a local Gradle 8.14.3 if there is one (mint), else the wrapper downloads it
if [ -z "${GRADLE:-}" ]; then
  GRADLE=./gradlew
  [ -x "$HOME/android/gradle-8.14.3/bin/gradle" ] && GRADLE="$HOME/android/gradle-8.14.3/bin/gradle"
fi

# One signing key forever, so updates install over the old app. Copy
# ~/.config/media-core/app.jks + app.jks.pass from mint; never make a new one
# by accident, or the TV and phone refuse the update.
CONF="$HOME/.config/media-core"
export MEDIA_CORE_KEYSTORE="$CONF/app.jks"
if [ ! -f "$MEDIA_CORE_KEYSTORE" ] || [ ! -f "$CONF/app.jks.pass" ]; then
  if [ "${MEDIA_CORE_NEW_KEY:-}" != 1 ]; then
    echo "no signing key in $CONF (app.jks + app.jks.pass)." >&2
    echo "Copy both from mint: scp 'mint:.config/media-core/app.jks*' $CONF/" >&2
    echo "Only for a brand-new install (apps signed with the old key must be uninstalled): MEDIA_CORE_NEW_KEY=1 $0 $*" >&2
    exit 1
  fi
  mkdir -p "$CONF"; umask 077
  head -c 24 /dev/urandom | base64 | tr -d '/+=' > "$CONF/app.jks.pass"
  keytool -genkeypair -keystore "$MEDIA_CORE_KEYSTORE" -alias media-core -keyalg RSA -keysize 3072 -validity 20000 \
    -storepass "$(cat "$CONF/app.jks.pass")" -keypass "$(cat "$CONF/app.jks.pass")" -dname "CN=media-core, O=mint" >/dev/null
  umask 022
fi
export MEDIA_CORE_KS_PASS="$(cat "$CONF/app.jks.pass")"

VCODE=$(date +%s)   # seconds: fits an int and rises on every build, so the app always offers a new one
VNAME=$(date +%Y.%m.%d-%H%M)
OUT="${MEDIA_CORE_APK_DIR:-$HOME/.local/share/media-core/app}"   # the engine serves the TV APK from here
mkdir -p "$OUT"

if [ "${1:-}" = "--phone" ]; then
  nice -n 10 "$GRADLE" --no-daemon -q :phone:assembleRelease -Pvcode="$VCODE" -Pvname="$VNAME"
  cp phone/build/outputs/apk/release/phone-release.apk "$OUT/media-core-phone.apk"
  echo "built phone $VNAME ($VCODE), $(du -h "$OUT/media-core-phone.apk" | cut -f1) -> $OUT/media-core-phone.apk"
  exit 0
fi

nice -n 10 "$GRADLE" --no-daemon -q :app:assembleTvRelease -Pvcode="$VCODE" -Pvname="$VNAME"
APK=app/build/outputs/apk/tv/release/app-tv-release.apk
cp "$APK" "$OUT/media-core.apk.tmp" && mv "$OUT/media-core.apk.tmp" "$OUT/media-core.apk"
printf '{"version_code":%s,"version_name":"%s","size":%s}\n' "$VCODE" "$VNAME" "$(wc -c < "$OUT/media-core.apk" | tr -d ' ')" > "$OUT/media-core.json"
echo "built $VNAME ($VCODE), $(du -h "$OUT/media-core.apk" | cut -f1) -> $OUT/media-core.apk"

if [ "${1:-}" = "--install" ] && [ -n "${2:-}" ]; then
  ADB=adb; command -v adb >/dev/null || ADB="$ANDROID_HOME/platform-tools/adb"
  "$ADB" connect "$2:5555" >/dev/null
  "$ADB" -s "$2:5555" install -r "$OUT/media-core.apk"
fi

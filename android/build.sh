#!/bin/bash
# Build the TV app and publish it on mint (the app updates itself from
# /api/v1/app.apk), or the separate phone app.
# Usage: android/build.sh [--install <tv-ip>] | android/build.sh --phone
#   --install: also push it to a device over adb (TV: Developer options > Network debugging)
#   --phone:   build the phone app instead -> ~/.local/share/theater/app/theater-phone.apk
set -euo pipefail
cd "$(dirname "$0")"
export ANDROID_HOME="$HOME/android/sdk" JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64
GRADLE="$HOME/android/gradle-8.14.3/bin/gradle"
CONF="$HOME/.config/theater"
export THEATER_KEYSTORE="$CONF/app.jks"

# one signing key forever, so updates install over the old app
if [ ! -f "$CONF/app.jks.pass" ]; then
  umask 077
  head -c 24 /dev/urandom | base64 | tr -d '/+=' > "$CONF/app.jks.pass"
  keytool -genkeypair -keystore "$THEATER_KEYSTORE" -alias mediadl -keyalg RSA -keysize 3072 -validity 20000 \
    -storepass "$(cat "$CONF/app.jks.pass")" -keypass "$(cat "$CONF/app.jks.pass")" -dname "CN=theater, O=mint" >/dev/null
fi
export THEATER_KS_PASS="$(cat "$CONF/app.jks.pass")"

VCODE=$(date +%s)   # seconds: fits an int and rises on every build, so the app always offers a new one
VNAME=$(date +%Y.%m.%d-%H%M)
OUT="$HOME/.local/share/theater/app"
mkdir -p "$OUT"

if [ "${1:-}" = "--phone" ]; then
  nice -n 10 "$GRADLE" --no-daemon -q assemblePhoneRelease -Pvcode="$VCODE" -Pvname="$VNAME"
  cp app/build/outputs/apk/phone/release/app-phone-release.apk "$OUT/theater-phone.apk"
  echo "built phone $VNAME ($VCODE), $(du -h "$OUT/theater-phone.apk" | cut -f1) -> $OUT/theater-phone.apk"
  exit 0
fi

nice -n 10 "$GRADLE" --no-daemon -q assembleTvRelease -Pvcode="$VCODE" -Pvname="$VNAME"
APK=app/build/outputs/apk/tv/release/app-tv-release.apk
cp "$APK" "$OUT/theater.apk.tmp" && mv "$OUT/theater.apk.tmp" "$OUT/theater.apk"
printf '{"version_code":%s,"version_name":"%s","size":%s}\n' "$VCODE" "$VNAME" "$(stat -c %s "$OUT/theater.apk")" > "$OUT/theater.json"
echo "built $VNAME ($VCODE), $(du -h "$OUT/theater.apk" | cut -f1) -> $OUT/theater.apk"

if [ "${1:-}" = "--install" ] && [ -n "${2:-}" ]; then
  adb connect "$2:5555" >/dev/null
  adb -s "$2:5555" install -r "$OUT/theater.apk"
fi

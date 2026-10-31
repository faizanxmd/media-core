#!/bin/bash
# Install the Android SDK pieces the apps need (Linux, no Android Studio).
# Gradle comes from android/gradlew; JDK 17 from your package manager
# (Mint/Ubuntu: sudo apt install openjdk-17-jdk-headless).
# On macOS, install Android Studio (or `brew install --cask android-commandlinetools`)
# and set ANDROID_HOME instead.
# Usage: scripts/android-setup.sh [--emulator]   (--emulator also fetches the TV + phone images)
set -euo pipefail
export ANDROID_HOME="${ANDROID_HOME:-$HOME/android/sdk}"
SM="$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager"
if [ ! -x "$SM" ]; then
  tmp=$(mktemp -d)
  curl -fsSLo "$tmp/clt.zip" https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip
  unzip -q "$tmp/clt.zip" -d "$tmp"
  mkdir -p "$ANDROID_HOME/cmdline-tools"
  rm -rf "$ANDROID_HOME/cmdline-tools/latest"
  mv "$tmp/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest"
  rm -rf "$tmp"
fi
yes | "$SM" --licenses >/dev/null || true
"$SM" "platforms;android-36" "build-tools;36.0.0" "platform-tools"
if [ "${1:-}" = "--emulator" ]; then
  # Android 11 TV: the Android 13 TV image crashes in host GPU emulation on mint
  "$SM" "emulator" "system-images;android-30;android-tv;x86" "system-images;android-30;google_apis;x86"
fi
echo "SDK ready in $ANDROID_HOME"

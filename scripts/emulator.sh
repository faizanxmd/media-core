#!/bin/bash
# Run a headless TV or phone emulator for UI testing (needs scripts/android-setup.sh --emulator).
# Usage: scripts/emulator.sh tv|phone
# On mint, run it as its own unit so it outlives the shell, and stop it after (~1.5 GB RAM):
#   systemd-run --user --unit=emu-tv --collect bash ~/apps/media-core/scripts/emulator.sh tv
#   adb -e ...;  systemctl --user stop emu-tv
set -euo pipefail
export ANDROID_HOME="${ANDROID_HOME:-$HOME/android/sdk}"
case "${1:-}" in
  tv)    avd=tv11;  image="system-images;android-30;android-tv;x86";   device=tv_1080p ;;
  phone) avd=phone; image="system-images;android-30;google_apis;x86"; device=pixel_6 ;;
  *) echo "usage: $0 tv|phone" >&2; exit 1 ;;
esac
if [ ! -d "$HOME/.android/avd/$avd.avd" ]; then
  echo no | "$ANDROID_HOME/cmdline-tools/latest/bin/avdmanager" create avd -n "$avd" -k "$image" -d "$device"
fi
cmd="$ANDROID_HOME/emulator/emulator -avd $avd -no-window -no-audio -no-boot-anim -gpu swiftshader_indirect \
  -feature -Vulkan,-GLDirectMem -memory 1280 -cores 2 -no-snapshot -no-metrics"
# /dev/kvm is group kvm; sg picks it up without logging out after `usermod -aG kvm`
if [ -e /dev/kvm ] && ! [ -w /dev/kvm ]; then exec sg kvm -c "$cmd"; fi
exec $cmd

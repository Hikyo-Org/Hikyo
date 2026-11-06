#!/bin/sh
# Build the debug APK. Run from spike/wails-shell with ANDROID_HOME and GRADLE_USER_HOME set and wails3 on PATH.
# build/android comes from `wails3 generate build-assets`; mobile-dev-config.json must exist (gitignored).
# usage: android-host/build.sh [stock]   (stock = the unpatched Wails Java host, for the baseline)
set -eu
tags=production,android,probe
[ "${1:-}" = stock ] && tags=$tags,stockhost
ndk=$(find "$ANDROID_HOME/ndk" -mindepth 1 -maxdepth 1 -type d | sort -V | tail -1)
cc=$ndk/toolchains/llvm/prebuilt/darwin-x86_64/bin/aarch64-linux-android24-clang
wails3 android overlay:gen >/dev/null
mkdir -p build/android/app/src/main/jniLibs/arm64-v8a
time CC=$cc CGO_ENABLED=1 GOOS=android GOARCH=arm64 go build -p 2 -buildmode=c-shared -overlay build/android/overlay.json \
  -tags "$tags" -trimpath -buildvcs=false -ldflags="-w -s" -o build/android/app/src/main/jniLibs/arm64-v8a/libwails.so
# stock builds the unpatched generated host (kept as MainActivity.java.orig once the patch has been applied)
app=build/android/app; java=$app/src/main/java/com/wails/app
if [ "${1:-}" = stock ]; then
  [ -f $java/MainActivity.java.orig ] && cp $java/MainActivity.java.orig $java/MainActivity.java
  rm -f $java/SpikeBridge.java $app/src/main/assets/spike-shim.js
  [ -f $java/WailsForegroundService.java.orig ] && cp $java/WailsForegroundService.java.orig $java/WailsForegroundService.java
  sed -i.bak 's/foregroundServiceType="shortService"/foregroundServiceType="dataSync"/' $app/src/main/AndroidManifest.xml
else
  app=build/android/app; java=$app/src/main/java/com/wails/app
  # idempotent: start from the generated MainActivity, then patch
  [ -f $java/MainActivity.java.orig ] || cp $java/MainActivity.java $java/MainActivity.java.orig
  cp $java/MainActivity.java.orig $java/MainActivity.java
  [ -f $java/WailsForegroundService.java.orig ] || cp $java/WailsForegroundService.java $java/WailsForegroundService.java.orig
  patch -s -p1 -d $java < android-host/MainActivity.patch
  cp android-host/java/SpikeBridge.java android-host/java/WailsForegroundService.java $java/
  sed -i.bak 's/foregroundServiceType="dataSync"/foregroundServiceType="shortService"/' $app/src/main/AndroidManifest.xml
  mkdir -p $app/src/main/assets && cp android-host/assets/spike-shim.js $app/src/main/assets/
  sed -i.bak 's/androidx.webkit:webkit:1.9.0/androidx.webkit:webkit:1.12.1/' $app/build.gradle
fi
(cd build/android && sh ./gradlew --no-daemon -q assembleDebug)
ls -l build/android/app/build/outputs/apk/debug/*.apk build/android/app/src/main/jniLibs/arm64-v8a/libwails.so

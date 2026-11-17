#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
tools="$PWD/.tools"
export ANDROID_HOME="${ANDROID_HOME:-$tools/sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/28.2.13676358}"
export GOPATH="${GOPATH:-$tools/gopath}" GOCACHE="${GOCACHE:-$tools/gocache}"
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$tools/gradle-home}"
export PATH="$tools/go/bin:$tools/bin:$PATH" GOMAXPROCS=2 GOFLAGS=-p=2
mkdir -p app/libs artifacts
(cd core; go test ./...; gomobile bind -target=android/arm64,android/amd64 -androidapi 26 -javapkg com.pbsx.core -o ../app/libs/mobilecore.aar ./mobilecore)
if [[ -x ./gradlew ]]; then gradle=./gradlew; else gradle="$tools/gradle-dist/gradle-8.13/bin/gradle"; fi
"$gradle" --no-daemon --max-workers=2 --console=plain :app:assembleDebug :app:testDebugUnitTest :app:lintDebug
cp app/build/outputs/apk/debug/app-debug.apk artifacts/pbsx-android-preview.apk
sha256sum artifacts/pbsx-android-preview.apk > artifacts/pbsx-android-preview.apk.sha256
python3 ../tools/android-build-info.py

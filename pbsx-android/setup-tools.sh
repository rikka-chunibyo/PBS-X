#!/usr/bin/env bash
# Optional isolated Linux build toolchain; no system or VM settings are changed.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
tools="$PWD/.tools"
mkdir -p "$tools/downloads" "$tools/bin" "$tools/sdk" "$tools/gradle-dist"
download() { if [[ ! -s "$2" ]]; then curl -fL --retry 2 -o "$2" "$1"; fi; }
download https://dl.google.com/android/repository/commandlinetools-linux-15859902_latest.zip "$tools/downloads/commandlinetools.zip"
echo "4e4c464f145a7512b57d088ac6c278c03c9eea610886b35a5e0804e74eedf583  $tools/downloads/commandlinetools.zip" | sha256sum -c -
if [[ ! -x "$tools/sdk/cmdline-tools/latest/bin/sdkmanager" ]]; then
    (cd "$tools/sdk"; jar -xf "$tools/downloads/commandlinetools.zip"; mv cmdline-tools cmdline-tools-extracted; mkdir cmdline-tools; mv cmdline-tools-extracted cmdline-tools/latest; chmod u+x cmdline-tools/latest/bin/*)
fi
download https://go.dev/dl/go1.27.3.linux-amd64.tar.gz "$tools/downloads/go.tar.gz"
# Checksums are supplied by the official Go release manifest over HTTPS.
download 'https://go.dev/dl/?mode=json&include=all' "$tools/downloads/go-releases.json"
python3 - "$tools" <<'PY'
import hashlib, json, pathlib, sys
root=pathlib.Path(sys.argv[1]); releases=json.loads((root/'downloads/go-releases.json').read_text())
entry=next(f for r in releases for f in r['files'] if f['filename']=='go1.27.3.linux-amd64.tar.gz')
if hashlib.sha256((root/'downloads/go.tar.gz').read_bytes()).hexdigest()!=entry['sha256']:
    raise SystemExit('Go download checksum mismatch')
PY
if [[ ! -x "$tools/go/bin/go" ]]; then tar -C "$tools" -xzf "$tools/downloads/go.tar.gz"; fi
download https://services.gradle.org/distributions/gradle-8.13-bin.zip "$tools/downloads/gradle.zip"
download https://services.gradle.org/distributions/gradle-8.13-bin.zip.sha256 "$tools/downloads/gradle.sha256"
echo "$(cat "$tools/downloads/gradle.sha256")  $tools/downloads/gradle.zip" | sha256sum -c -
if [[ ! -x "$tools/gradle-dist/gradle-8.13/bin/gradle" ]]; then
    (cd "$tools/gradle-dist"; jar -xf "$tools/downloads/gradle.zip"; chmod u+x gradle-8.13/bin/gradle)
fi
export ANDROID_HOME="$tools/sdk" GOPATH="$tools/gopath" GOCACHE="$tools/gocache" GOBIN="$tools/bin"
export PATH="$tools/go/bin:$tools/bin:$PATH" GOMAXPROCS=2 GOFLAGS=-p=2
# Review Android's SDK licenses interactively before installation.
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --sdk_root="$ANDROID_HOME" --licenses
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --sdk_root="$ANDROID_HOME" 'platform-tools' 'platforms;android-36' 'build-tools;36.0.0' 'ndk;28.2.13676358'
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867
go install golang.org/x/mobile/cmd/gobind@v0.0.0-20261011034528-197770444867

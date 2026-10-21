#!/bin/sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir=${1:-"$project_root/build"}
bundle="$output_dir/KubePin.app"
binary_dir="$bundle/Contents/MacOS"
resource_dir="$bundle/Contents/Resources"

mkdir -p "$binary_dir" "$resource_dir"
cp "$project_root/macos/Info.plist" "$bundle/Contents/Info.plist"

machine_arch=$(uname -m)
case "$machine_arch" in
    arm64) go_arch=arm64 ;;
    x86_64) go_arch=amd64 ;;
    *) echo "Unsupported Mac architecture: $machine_arch" >&2; exit 1 ;;
esac

(cd "$project_root" && GOOS=darwin GOARCH="$go_arch" go build -o "$resource_dir/kubepin" .)
xcrun swiftc \
    -target "$machine_arch-apple-macosx14.0" \
    -O \
    -framework AppKit \
    -framework SwiftUI \
    -o "$binary_dir/KubePin" \
    "$project_root"/macos/Sources/*.swift
codesign --force --deep --sign - "$bundle"
echo "$bundle"

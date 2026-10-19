#!/bin/sh
# Builds the release downloads into dist/:
#
#   Dasher-D200-macOS.zip          universal app (Apple Silicon + Intel)
#   Dasher-D200-Windows.zip        x64 and arm64 .exe, with SHA256SUMS.txt
#   Dasher-D200-RaspberryPi.tar.gz 64-bit (arm64) and 32-bit (armhf)
#   Dasher-D200-Linux.tar.gz       x86-64
#
# Needs Go 1.25+, zip, tar, sha256sum (or shasum), and for the Mac app
# lipo (on a Mac) or llvm-lipo (elsewhere). No C compiler is needed: every
# build is pure Go, cross-compiled from whatever machine runs this.
#
# The version comes from main.go. To refresh the Windows icon and version
# details after changing the version, run: ./build.sh winres
# (needs llvm-windres).
set -eu
cd "$(dirname "$0")"

VERSION=$(sed -n 's/^var version = "\(.*\)"/\1/p' main.go)
[ -n "$VERSION" ] || { echo "can't find the version in main.go" >&2; exit 1; }
IFS=. read -r MAJ MIN PAT <<EOF
$VERSION
EOF
BUILD=$((MAJ * 10000 + MIN * 100 + PAT))   # CFBundleVersion, always increasing
export CGO_ENABLED=0

if [ "${1:-}" = winres ]; then
    WINRES=$(command -v llvm-windres || command -v llvm-windres-18 || true)
    [ -n "$WINRES" ] || { echo "llvm-windres not found" >&2; exit 1; }
    sed -i.bak "s/[0-9]*,[0-9]*,[0-9]*,0/$MAJ,$MIN,$PAT,0/g; s/\"[0-9]*\.[0-9]*\.[0-9]*\"/\"$VERSION\"/g" winres/dasher.rc
    sed -i.bak "s/version=\"[0-9.]*\"/version=\"$VERSION.0\"/" winres/app.manifest
    rm -f winres/*.bak
    (cd winres &&
        "$WINRES" --no-preprocess --target=x86_64-w64-mingw32 -O coff dasher.rc -o ../rsrc_windows_amd64.syso &&
        "$WINRES" --no-preprocess --target=aarch64-w64-mingw32 -O coff dasher.rc -o ../rsrc_windows_arm64.syso)
    echo "Windows resources updated to $VERSION"
    exit 0
fi

LIPO=$(command -v lipo || command -v llvm-lipo || command -v llvm-lipo-18 || true)
SHA=$(command -v sha256sum >/dev/null && echo sha256sum || echo "shasum -a 256")

rm -rf build dist
mkdir -p build dist
b() { # b GOOS GOARCH output [extra ldflags]
    echo "  $1/$2"
    GOOS=$1 GOARCH=$2 GOARM=7 go build -trimpath -ldflags="${4:--s -w}" -o "build/$3" .
}
echo "Building Dasher D200 $VERSION"
go test ./dasher ./link
b darwin arm64 mac-arm64
b darwin amd64 mac-amd64
b windows amd64 Dasher200-x64.exe "-H windowsgui"
b windows arm64 Dasher200-arm64.exe "-H windowsgui"
b linux amd64 dasher200
b linux arm64 dasher200-arm64
b linux arm dasher200-armhf

# macOS: universal app bundle.
APP="build/mac/Dasher D200.app"
mkdir -p "$APP/Contents/MacOS"
if [ -n "$LIPO" ]; then
    "$LIPO" -create build/mac-arm64 build/mac-amd64 -output "$APP/Contents/MacOS/dasher200"
else
    echo "  (no lipo found: the Mac app will be Apple Silicon only)"
    cp build/mac-arm64 "$APP/Contents/MacOS/dasher200"
fi
sed "s/@VERSION@/$VERSION/; s/@BUILD@/$BUILD/" packaging/macos/Info.plist > "$APP/Contents/Info.plist"
cp "packaging/macos/Sign Dasher D200.command" README.md build/mac/
chmod +x "build/mac/Sign Dasher D200.command"
(cd build/mac && zip -qry9 ../../dist/Dasher-D200-macOS.zip "Dasher D200.app" "Sign Dasher D200.command" README.md)

# Windows.
W=build/win/Dasher-D200-Windows
mkdir -p $W
cp build/Dasher200-x64.exe build/Dasher200-arm64.exe README.md $W/
(cd $W && $SHA Dasher200-x64.exe Dasher200-arm64.exe | sed 's/$/\r/' > SHA256SUMS.txt)
(cd build/win && zip -qr9 ../../dist/Dasher-D200-Windows.zip Dasher-D200-Windows)

# Raspberry Pi and Linux.
P=build/pi/Dasher-D200-RaspberryPi
mkdir -p $P
cp build/dasher200-arm64 build/dasher200-armhf README.md $P/
(cd build/pi && tar czf ../../dist/Dasher-D200-RaspberryPi.tar.gz Dasher-D200-RaspberryPi)
L=build/lin/Dasher-D200-Linux
mkdir -p $L
cp build/dasher200 README.md $L/
(cd build/lin && tar czf ../../dist/Dasher-D200-Linux.tar.gz Dasher-D200-Linux)

(cd dist && $SHA * > SHA256SUMS.txt)
echo "Done:"
ls -l dist

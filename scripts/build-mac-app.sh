#!/bin/bash
# Builds Matinee Server.app, the menu-bar app that runs the server on a Mac,
# and its disk image, into dist/mac. Run on a Mac after `make web`.
#
#   VERSION=0.2.0 scripts/build-mac-app.sh
#
# Universal (arm64 + x86_64): the server, FFmpeg (scripts/build-mac-ffmpeg.sh,
# or a folder of prebuilt ones in FFMPEG_DIR) and the app from macos/.
#
# Each of these is optional; without them the app is built unsigned, for
# trying out and for CI on pull requests:
#   MAC_SIGN_IDENTITY       Developer ID Application identity (its SHA-1)
#   ASC_API_KEY, ASC_API_KEY_ID, ASC_API_ISSUER_ID
#                           App Store Connect API key (.p8 path) to notarize
#   SPARKLE_PRIVATE_KEY     update signing key: turns on updates and writes
#                           appcast.xml for the release
set -euo pipefail
: "${VERSION:?set VERSION, e.g. 0.2.0}"
test -f web/dist/index.html || { echo "build the web player first: make web" >&2; exit 1; }

root="$(pwd)"
out="$root/dist/mac"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
rm -rf "$out" && mkdir -p "$out"
app="$out/Matinee Server.app"
contents="$app/Contents"
mkdir -p "$contents/MacOS" "$contents/Frameworks" "$contents/Resources"

echo "::group::Server"
for arch in arm64 amd64; do
	CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=$VERSION -X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=${TMDB_KEY:-}" \
		-o "$work/matinee-$arch" ./cmd/matinee
done
lipo -create -output "$contents/MacOS/matinee" "$work/matinee-arm64" "$work/matinee-amd64"
echo "::endgroup::"

echo "::group::FFmpeg"
ffmpeg_dir="${FFMPEG_DIR:-$work/ffmpeg}"
[ -x "$ffmpeg_dir/ffmpeg" ] || scripts/build-mac-ffmpeg.sh "$ffmpeg_dir"
cp "$ffmpeg_dir/ffmpeg" "$ffmpeg_dir/ffprobe" "$contents/MacOS/"
cp "$ffmpeg_dir/FFmpeg.txt" "$contents/Resources/"
echo "::endgroup::"

echo "::group::App"
swift build --package-path macos -c release --arch arm64 --arch x86_64
products="$(swift build --package-path macos -c release --arch arm64 --arch x86_64 --show-bin-path)"
cp "$products/MatineeServer" "$contents/MacOS/Matinee Server"
sparkle="$(find "$products" macos/.build/artifacts -name Sparkle.framework -type d -prune 2>/dev/null | head -n 1)"
[ -n "$sparkle" ] || { echo "::error::Sparkle.framework not found in the build"; exit 1; }
ditto "$sparkle" "$contents/Frameworks/Sparkle.framework"

public_key=""
if [ -n "${SPARKLE_PRIVATE_KEY:-}" ]; then
	public_key="$(swift macos/Resources/sparkle.swift public-key)"
fi
sed -e "s|@VERSION@|$VERSION|g" -e "s|@SPARKLE_PUBLIC_KEY@|$public_key|g" macos/Resources/Info.plist > "$contents/Info.plist"
plutil -lint "$contents/Info.plist"

iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
	sips -z $size $size macos/Resources/AppIcon.png --out "$iconset/icon_${size}x${size}.png" > /dev/null
	sips -z $((size * 2)) $((size * 2)) macos/Resources/AppIcon.png --out "$iconset/icon_${size}x${size}@2x.png" > /dev/null
done
iconutil -c icns -o "$contents/Resources/AppIcon.icns" "$iconset"
cp LICENSE "$contents/Resources/LICENSE.txt"
echo "::endgroup::"

echo "::group::Sign"
identity="${MAC_SIGN_IDENTITY:-}"
if [ -n "$identity" ]; then
	sign() { codesign --force --sign "$identity" --options runtime --timestamp "$@"; }
	# Sparkle's pieces inside out, as its documentation says for apps
	# signed outside Xcode.
	fw="$contents/Frameworks/Sparkle.framework/Versions/B"
	[ -d "$fw/XPCServices/Installer.xpc" ] && sign "$fw/XPCServices/Installer.xpc"
	[ -d "$fw/XPCServices/Downloader.xpc" ] && sign --preserve-metadata=entitlements "$fw/XPCServices/Downloader.xpc"
	sign "$fw/Autoupdate"
	sign "$fw/Updater.app"
	sign "$contents/Frameworks/Sparkle.framework"
	for tool in matinee ffmpeg ffprobe; do sign "$contents/MacOS/$tool"; done
	sign --entitlements macos/Resources/entitlements.plist "$app"
else
	echo "::notice::No MAC_SIGN_IDENTITY: the app is signed ad hoc and runs only on this Mac (or after right-click → Open)."
	codesign --force --deep --sign - "$app"
fi
codesign --verify --deep --strict --verbose=2 "$app"
echo "::endgroup::"

# notarize <file>: sends it to Apple's notary service and staples the ticket.
notarize() {
	local file="$1" result id status
	result="$(xcrun notarytool submit "$file" --key "$ASC_API_KEY" --key-id "$ASC_API_KEY_ID" --issuer "$ASC_API_ISSUER_ID" --wait --output-format json)"
	id="$(plutil -extract id raw - <<< "$result" 2>/dev/null || true)"
	status="$(plutil -extract status raw - <<< "$result" 2>/dev/null || true)"
	if [ "$status" != Accepted ]; then
		echo "$result"
		[ -n "$id" ] && xcrun notarytool log "$id" --key "$ASC_API_KEY" --key-id "$ASC_API_KEY_ID" --issuer "$ASC_API_ISSUER_ID" || true
		echo "::error::Notarizing $(basename "$file") came back ${status:-with no status}; the notary log is in the job log."
		exit 1
	fi
}
# staple <file>: attaches the notary ticket. Apple's ticket service can lag
# right after notarizing, which stapler reports as error 68, so it tries again.
staple() {
	local try
	for try in 1 2 3 4 5 6; do
		xcrun stapler staple "$1" && return 0
		echo "Stapling $(basename "$1") failed (try $try); waiting for the ticket"
		sleep 30
	done
	echo "::error::Couldn't staple the notary ticket to $(basename "$1")."
	exit 1
}
notary=""
if [ -n "$identity" ] && [ -n "${ASC_API_KEY:-}" ] && [ -n "${ASC_API_KEY_ID:-}" ] && [ -n "${ASC_API_ISSUER_ID:-}" ]; then
	notary=1
fi

if [ -n "$notary" ]; then
	echo "::group::Notarize the app"
	ditto -c -k --keepParent "$app" "$work/app.zip"
	notarize "$work/app.zip"
	staple "$app"
	echo "::endgroup::"
fi

echo "::group::Disk image"
staging="$work/dmg"
mkdir -p "$staging"
ditto "$app" "$staging/Matinee Server.app"
ln -s /Applications "$staging/Applications"
dmg="$out/Matinee-Server.dmg"
hdiutil create -quiet -volname "Matinee Server" -srcfolder "$staging" -fs HFS+ -format UDZO -ov "$dmg"
if [ -n "$identity" ]; then
	codesign --force --sign "$identity" --timestamp "$dmg"
fi
if [ -n "$notary" ]; then
	notarize "$dmg"
	staple "$dmg"
	spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
fi
echo "::endgroup::"

if [ -n "$public_key" ]; then
	echo "::group::Update feed"
	signature="$(swift macos/Resources/sparkle.swift sign "$dmg")"
	length="$(stat -f %z "$dmg")"
	cat > "$out/appcast.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>Matinee Server</title>
    <link>https://matinee.yeix.io</link>
    <item>
      <title>Matinee ${VERSION}</title>
      <pubDate>$(LC_ALL=C date -u '+%a, %d %b %Y %H:%M:%S +0000')</pubDate>
      <sparkle:version>${VERSION}</sparkle:version>
      <sparkle:shortVersionString>${VERSION}</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>13.0</sparkle:minimumSystemVersion>
      <sparkle:releaseNotesLink>https://github.com/yeixio/matinee/releases/tag/v${VERSION}</sparkle:releaseNotesLink>
      <enclosure url="https://github.com/yeixio/matinee/releases/download/v${VERSION}/Matinee-Server.dmg" length="${length}" type="application/x-apple-diskimage" sparkle:edSignature="${signature}"/>
    </item>
  </channel>
</rss>
XML
	xmllint --noout "$out/appcast.xml"
	echo "::endgroup::"
fi
ls -l "$out"

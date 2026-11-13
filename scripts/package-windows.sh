#!/bin/sh
# Builds the Windows installers into dist/: Matinee-Setup-x64.exe and
# Matinee-Setup-arm64.exe, each with the server, the tray app
# (cmd/matinee-tray) and FFmpeg. Needs Go, makensis (NSIS 3), curl and unzip; run `make web` first.
# Usage: VERSION=1.2.3 scripts/package-windows.sh
# ARCHES="x64" builds only that one. With WINDOWS_SIGNING set up (see
# scripts/sign-windows.sh), the programs and installers are signed.
set -eu
: "${VERSION:?set VERSION, e.g. 1.2.3}"
test -f web/dist/index.html || { echo "build the web player first: make web" >&2; exit 1; }

# FFmpeg's own Windows builds, from the FFmpeg 8.1 release branch, GPL so
# H.264 and HEVC encoding work. The builds are rebuilt daily under the same
# names, so each download is checked against the same day's checksums.
FFMPEG_BASE=https://github.com/BtbN/FFmpeg-Builds/releases/download/latest
FFMPEG_SERIES=n8.1

# Windows version numbers are four numbers, so 0.2.0-beta.1 is 0.2.0.0.
numver="${VERSION%%-*}.0"

root=$(pwd)
mkdir -p dist
cache="${FFMPEG_CACHE:-$root/.cache/ffmpeg}"
mkdir -p "$cache"
curl -fsSL "$FFMPEG_BASE/checksums.sha256" -o "$cache/checksums.sha256"

# The game runner and emulators for Windows, built once by the "Game
# emulators" and "Arcade emulator" workflows. They're x64 builds and go in
# both installers: Windows on Arm runs them as it runs any x64 program. A
# release fails without them (REQUIRE_GAMES=1); other builds leave games
# out and say so.
releases="https://github.com/yeixio/matinee/releases/download"
games_tag="$(sh games/cores-tag.sh games)"
mame_tag="mame-core-$(sed -n 's/^version=//p' games/mame.txt)-$(sed -n 's/^commit=//p' games/mame.txt | cut -c1-7)"
mame_b="$(sed -n 's/^build=//p' games/mame.txt)"
mame_tag="$mame_tag${mame_b:+-$mame_b}"
games="$cache/games-$games_tag"
if [ ! -f "$games/matinee-game-runner.exe" ]; then
	rm -rf "$games" "$games.tmp"
	mkdir -p "$games.tmp"
	f=matinee-games-windows-amd64.tar.xz
	if curl -fsSL -o "$cache/$f" "$releases/$games_tag/$f" &&
		curl -fsSL -o "$cache/games-SHA256SUMS" "$releases/$games_tag/SHA256SUMS"; then
		(cd "$cache" && grep " $f\$" games-SHA256SUMS | sha256sum -c -)
		tar -xJf "$cache/$f" -C "$games.tmp"
		m=mame_libretro-windows-amd64.so.xz
		if curl -fsSL -o "$cache/$m" "$releases/$mame_tag/$m" &&
			curl -fsSL -o "$cache/mame-SHA256SUMS" "$releases/$mame_tag/SHA256SUMS"; then
			(cd "$cache" && grep " $m\$" mame-SHA256SUMS | sha256sum -c -)
			xz -d -c "$cache/$m" > "$games.tmp/cores/mame_libretro.so"
			curl -fsSL -o "$games.tmp/licenses/mame.txt" "$releases/$mame_tag/mame-COPYING.txt"
		elif [ -n "${REQUIRE_GAMES:-}" ]; then
			echo "no Windows arcade emulator in $mame_tag" >&2
			exit 1
		else
			echo "no Windows arcade emulator in $mame_tag: packaging without it"
		fi
		mv "$games.tmp" "$games"
	elif [ -n "${REQUIRE_GAMES:-}" ]; then
		echo "no $games_tag release with Windows emulators" >&2
		exit 1
	else
		rm -rf "$games.tmp"
		echo "no $games_tag release with Windows emulators: packaging without games"
	fi
fi

sign=""
if [ -n "${WINDOWS_SIGNING:-}" ]; then
	sign="$root/scripts/sign-windows.sh"
fi

for arch in ${ARCHES:-x64 arm64}; do
	case $arch in
	x64) goarch=amd64 ffarch=win64 ;;
	arm64) goarch=arm64 ffarch=winarm64 ;;
	*) echo "unknown arch $arch" >&2; exit 1 ;;
	esac
	stage=$(mktemp -d)
	trap 'rm -rf "$stage"' EXIT

	CGO_ENABLED=0 GOOS=windows GOARCH=$goarch go build -trimpath \
		-ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=$VERSION -X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=${TMDB_KEY:-}" \
		-o "$stage/matinee.exe" ./cmd/matinee

	# The tray app's icon, version and manifest go in a resource file next
	# to its code, which go build links in.
	go run github.com/tc-hib/go-winres@v0.3.3 simply \
		--arch "$goarch" --out cmd/matinee-tray/rsrc \
		--icon cmd/matinee-tray/icons/app-256.png --manifest gui \
		--product-name Matinee --file-description Matinee \
		--product-version "$numver" --file-version "$numver" \
		--original-filename matinee-tray.exe --copyright "AGPL-3.0-or-later"
	CGO_ENABLED=0 GOOS=windows GOARCH=$goarch go build -trimpath \
		-ldflags "-s -w -H=windowsgui -X github.com/yeixio/matinee/internal/version.Version=$VERSION" \
		-o "$stage/matinee-tray.exe" ./cmd/matinee-tray
	rm -f cmd/matinee-tray/rsrc_windows_*.syso

	zip="ffmpeg-$FFMPEG_SERIES-latest-$ffarch-gpl-shared-${FFMPEG_SERIES#n}.zip"
	want=$(awk -v f="$zip" '$2 == f { print $1 }' "$cache/checksums.sha256")
	test -n "$want" || { echo "no checksum for $zip" >&2; exit 1; }
	if [ ! -f "$cache/$zip" ] || [ "$(sha256sum "$cache/$zip" | cut -d' ' -f1)" != "$want" ]; then
		curl -fsSL "$FFMPEG_BASE/$zip" -o "$cache/$zip"
	fi
	echo "$want  $cache/$zip" | sha256sum -c -
	unzip -q -o "$cache/$zip" -d "$stage/unzip"
	mkdir -p "$stage/ffmpeg"
	src="$stage/unzip/${zip%.zip}"
	cp "$src/bin/ffmpeg.exe" "$src/bin/ffprobe.exe" "$src"/bin/*.dll "$stage/ffmpeg/"
	cp "$src/LICENSE.txt" "$stage/ffmpeg/LICENSE.txt"
	cat > "$stage/ffmpeg/README.txt" <<-TXT
		This is FFmpeg ($FFMPEG_SERIES), built by https://github.com/BtbN/FFmpeg-Builds
		and licensed under the GNU GPL version 3 (LICENSE.txt). Its source code is
		at https://ffmpeg.org/download.html and https://github.com/FFmpeg/FFmpeg.
	TXT
	rm -rf "$stage/unzip"
	cp LICENSE "$stage/LICENSE.txt"
	if [ -d "$games" ]; then
		cp -R "$games" "$stage/games"
	fi

	if [ -n "$sign" ]; then
		"$sign" "$stage/matinee.exe" "$stage/matinee-tray.exe"
	fi
	out="$root/dist/Matinee-Setup-$arch.exe"
	makensis -V2 -INPUTCHARSET UTF8 \
		-DVERSION="$VERSION" -DNUMVER="$numver" -DARCH="$arch" -DSTAGE="$stage" -DOUTFILE="$out" \
		${sign:+-DSIGN="$sign"} packaging/windows/matinee.nsi
	rm -rf "$stage"
	trap - EXIT
done
ls -l dist/Matinee-Setup-*

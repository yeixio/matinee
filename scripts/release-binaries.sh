#!/bin/sh
# Builds release archives of the server, web player included, into dist/.
# Usage: VERSION=1.2.3 scripts/release-binaries.sh  (run `make web` first)
set -eu
: "${VERSION:?set VERSION, e.g. 1.2.3}"
test -f web/dist/index.html || { echo "build the web player first: make web" >&2; exit 1; }

rm -rf dist && mkdir -p dist
for target in linux/amd64 linux/arm64 linux/arm/7 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	os=${target%%/*}
	rest=${target#*/}
	arch=${rest%%/*}
	arm=""
	[ "$arch" = arm ] && arm=${rest#*/}
	name="matinee_${VERSION}_${os}_${arch}${arm:+v$arm}"
	exe=matinee
	[ "$os" = windows ] && exe=matinee.exe
	mkdir -p "dist/$name"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOARM=$arm go build -trimpath \
		-ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=$VERSION -X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=${TMDB_KEY:-}" \
		-o "dist/$name/$exe" ./cmd/matinee
	cp LICENSE README.md "dist/$name/"
	if [ "$os" = windows ]; then
		(cd dist && zip -qr "$name.zip" "$name")
	else
		tar -C dist -czf "dist/$name.tar.gz" "$name"
	fi
	rm -rf "dist/$name"
done
(cd dist && sha256sum ./*.tar.gz ./*.zip | sed 's| \./| |' > SHA256SUMS.txt)
ls -l dist

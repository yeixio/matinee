# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
# The text in every language, shared with the server and the apps.
COPY i18n/ /src/i18n/
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG TARGETOS TARGETARCH TARGETVARIANT
# Official images get the project's TMDB key from a build secret (kept out of image history).
RUN --mount=type=secret,id=tmdb_key \
    TMDB_KEY="$(cat /run/secrets/tmdb_key 2>/dev/null || true)" && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM="${TARGETVARIANT#v}" go build -trimpath \
    -ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=${VERSION} -X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=${TMDB_KEY}" \
    -o /out/matinee ./cmd/matinee
# The game host for PC games (docs/guide/server/games.md), shipped in the
# image so a Steam container can copy it in.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM="${TARGETVARIANT#v}" go build -trimpath \
    -ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=${VERSION}" \
    -o /out/matinee-gamehost ./cmd/matinee-gamehost

# The game runner and the emulators that play games on the server
# (games/). Built on the build machine for the target, with a cross
# compiler, which is much faster than building under emulation.
FROM --platform=$BUILDPLATFORM debian:trixie AS games
# libgl-dev only for its headers: the N64 emulator, built without OpenGL,
# still includes them. nasm assembles its x86-64 recompiler.
ARG TARGETARCH BUILDARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git make cmake gcc g++ binutils libgl-dev nasm \
    && case "$TARGETARCH" in \
         "$BUILDARCH") ;; \
         arm64) apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu g++-aarch64-linux-gnu binutils-aarch64-linux-gnu ;; \
         arm) apt-get install -y --no-install-recommends gcc-arm-linux-gnueabihf g++-arm-linux-gnueabihf binutils-arm-linux-gnueabihf ;; \
         amd64) apt-get install -y --no-install-recommends gcc-x86-64-linux-gnu g++-x86-64-linux-gnu binutils-x86-64-linux-gnu ;; \
       esac \
    && rm -rf /var/lib/apt/lists/*
COPY games/ /src/games/
RUN case "$TARGETARCH" in \
      "$BUILDARCH") host= ;; \
      arm64) host=aarch64-linux-gnu ;; \
      arm) host=arm-linux-gnueabihf ;; \
      amd64) host=x86_64-linux-gnu ;; \
    esac \
    && HOST=$host sh /src/games/build.sh /out/games
# The arcade emulator, MAME, is built on its own because it takes hours
# (games/mame.txt, .github/workflows/mame-core.yml): fetched from its
# release, for 64-bit machines. Until that release exists the image has no
# arcade emulator, and arcade games say so. (A layer cached from before a
# release existed has no MAME: change this step to rebuild it.)
RUN apt-get update && apt-get install -y --no-install-recommends curl xz-utils && rm -rf /var/lib/apt/lists/* \
    && v=$(sed -n 's/^version=//p' /src/games/mame.txt) && c=$(sed -n 's/^commit=//p' /src/games/mame.txt | cut -c1-7) \
    && base="https://github.com/yeixio/matinee/releases/download/mame-core-$v-$c" \
    && case "$TARGETARCH" in amd64|arm64) \
         if curl -fsSL -o /tmp/mame.so.xz "$base/mame_libretro-$TARGETARCH.so.xz" && curl -fsSL -o /tmp/SHA256SUMS "$base/SHA256SUMS"; then \
           (cd /tmp && grep " mame_libretro-$TARGETARCH.so.xz\$" SHA256SUMS | sed "s/mame_libretro-$TARGETARCH.so.xz/mame.so.xz/" | sha256sum -c -) \
           && xz -d -c /tmp/mame.so.xz > /out/games/cores/mame_libretro.so \
           && curl -fsSL -o /out/games/licenses/mame.txt "$base/mame-COPYING.txt" \
           && echo "MAME $v ($c) added"; \
         else echo "no MAME release mame-core-$v-$c yet"; fi ;; \
       esac

# Debian rather than Alpine: its FFmpeg is built with every hardware backend
# Matinee uses on Linux, and glibc lets NVIDIA's container runtime add its
# driver libraries. mesa-va-drivers runs VAAPI on AMD GPUs (and older Intel);
# intel-media-va-driver runs VAAPI on newer Intel ones (Matinee prefers VAAPI
# to Quick Sync on Linux).
FROM debian:trixie-slim
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg mesa-va-drivers ca-certificates tzdata wget \
    && if [ "$TARGETARCH" = amd64 ]; then apt-get install -y --no-install-recommends intel-media-va-driver; fi \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -u 1000 -U -m -d /home/matinee matinee \
    && mkdir -p /data && chown matinee:matinee /data
# Tells NVIDIA's container runtime to add the video encode/decode libraries.
ENV NVIDIA_DRIVER_CAPABILITIES=compute,video,utility
COPY --from=server /out/matinee /usr/local/bin/matinee
COPY --from=server /out/matinee-gamehost /usr/local/bin/matinee-gamehost
COPY --from=games /out/games /usr/lib/matinee/games
USER matinee
# MATINEE_INSTALL tells Settings → Updates that a new image is the way to update.
ENV MATINEE_DATA_DIR=/data MATINEE_ADDR=:7171 MATINEE_INSTALL=docker
VOLUME /data
# 7173/udp streams games to players.
EXPOSE 7171 7173/udp
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:7171/healthz || exit 1
ENTRYPOINT ["matinee"]

# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
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
USER matinee
ENV MATINEE_DATA_DIR=/data MATINEE_ADDR=:7171
VOLUME /data
EXPOSE 7171
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:7171/healthz || exit 1
ENTRYPOINT ["matinee"]

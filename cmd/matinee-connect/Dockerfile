# syntax=docker/dockerfile:1
# The rendezvous service (internal/connect). Build from the repository root:
#   docker build -f cmd/matinee-connect/Dockerfile .

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/matinee-connect ./cmd/matinee-connect
COPY internal/connect ./internal/connect
COPY internal/version ./internal/version
ARG VERSION=dev
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=${VERSION}" \
    -o /out/matinee-connect ./cmd/matinee-connect

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/matinee-connect /matinee-connect
EXPOSE 8080
ENTRYPOINT ["/matinee-connect"]

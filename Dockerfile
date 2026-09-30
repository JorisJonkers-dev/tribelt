# syntax=docker/dockerfile:1.10
# Cross-compiles on the build platform, so a multi-arch build needs no emulation for the Go stage.
# content/ is embedded into the binary: one image is exactly one Content Release.

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/tribelt ./cmd/tribelt

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tribelt /tribelt
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tribelt"]
CMD ["serve"]

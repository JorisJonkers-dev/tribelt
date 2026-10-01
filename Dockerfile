# syntax=docker/dockerfile:1.10
# Cross-compiles on the build platform, so a multi-arch build needs no emulation for the Go stage.
# content/ is embedded into the binary: one image is exactly one Content Release.

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
# Tailwind standalone CLI for the public stylesheet; keep the version in step with mise.toml.
ARG BUILDARCH
ARG TAILWIND_VERSION=4.3.3
RUN set -eu; \
    case "${BUILDARCH}" in \
      amd64) arch=x64; sum=a04d34ceacc8f52cbe8920ad846cdeb61d3d0021dba32db0d1f77c9d9fad7a6c ;; \
      arm64) arch=arm64; sum=71ea4be79c9de9827545682df3e040053fb535d37c71ed2cfdedf9385a0868e0 ;; \
      *) echo "no Tailwind CLI for ${BUILDARCH}" >&2; exit 1 ;; \
    esac; \
    apk add --no-cache libstdc++ libgcc; \
    wget -qO /usr/local/bin/tailwindcss "https://github.com/tailwindlabs/tailwindcss/releases/download/v${TAILWIND_VERSION}/tailwindcss-linux-${arch}-musl"; \
    echo "${sum}  /usr/local/bin/tailwindcss" | sha256sum -c -; \
    chmod +x /usr/local/bin/tailwindcss
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN tailwindcss --input web/css/site.css --output web/static/site.css --minify \
 && tailwindcss --input web/css/stats.css --output web/static/stats.css --minify
ARG TARGETOS
ARG TARGETARCH
# The App Version is embedded from .release-please-manifest.json, so no version build argument is needed.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/tribelt ./cmd/tribelt

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tribelt /tribelt
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tribelt"]
CMD ["serve"]

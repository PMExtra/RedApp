FROM --platform=$BUILDPLATFORM golang:1.25.1-bookworm AS build
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG BUILDARCH
RUN set -eu; \
    case "$TARGETOS/$TARGETARCH" in linux/amd64|linux/arm64) ;; *) echo "Unsupported target: $TARGETOS/$TARGETARCH" >&2; exit 1 ;; esac; \
    compiler=gcc; \
    if [ "$BUILDARCH" != "$TARGETARCH" ]; then \
      case "$TARGETARCH" in amd64) compiler=x86_64-linux-gnu-gcc ;; arm64) compiler=aarch64-linux-gnu-gcc ;; esac; \
      apt-get update && apt-get install -y --no-install-recommends "gcc-${compiler%-gcc}" libc6-dev-${TARGETARCH}-cross; \
      rm -rf /var/lib/apt/lists/*; \
    fi; \
    CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH CC=$compiler go build -tags netgo,osusergo,sqlite_omit_load_extension -trimpath -ldflags="-linkmode external -extldflags '-static' -X main.version=${VERSION} -X main.revision=${REVISION}" -o /redapp ./cmd/redapp \
    && mkdir -p /var/lib/redapp && chmod 0700 /var/lib/redapp

FROM scratch AS runtime
ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="RedApp" \
      org.opencontainers.image.source="https://github.com/PMExtra/RedApp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /redapp /redapp
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /var/lib/redapp /var/lib/redapp
USER 65532:65532
ENV REDAPP_DATA=/var/lib/redapp REDAPP_LISTEN=:8080 REDAPP_PUBLIC_URL=http://localhost:8080
VOLUME ["/var/lib/redapp"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/redapp", "healthcheck"]
ENTRYPOINT ["/redapp"]

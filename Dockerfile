FROM golang:1.25.1-bookworm AS build
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -tags netgo,osusergo,sqlite_omit_load_extension -trimpath -ldflags="-linkmode external -extldflags '-static' -X main.version=${VERSION} -X main.revision=${REVISION}" -o /redapp ./cmd/redapp \
    && mkdir -p /image-data && chmod 0700 /image-data

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
COPY --from=build --chown=65532:65532 /image-data /data
USER 65532:65532
ENV REDAPP_DATA=/data REDAPP_LISTEN=:8080 REDAPP_PUBLIC_URL=http://localhost:8080
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/redapp", "healthcheck"]
ENTRYPOINT ["/redapp"]

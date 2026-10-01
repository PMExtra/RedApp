FROM node:24.19.0-trixie-slim AS frontend
WORKDIR /frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27.1-trixie AS build
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /internal/httpserver/web ./internal/httpserver/web
RUN CGO_ENABLED=1 go build -tags netgo,osusergo,sqlite_omit_load_extension -trimpath -ldflags="-linkmode external -extldflags '-static' -X main.version=${VERSION} -X main.revision=${REVISION}" -o /redapp ./cmd/redapp \
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
VOLUME ["/var/lib/redapp"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/redapp", "healthcheck"]
ENTRYPOINT ["/redapp"]

# Base images are pinned only here. scripts/build-native-container.sh and the CI
# cache key read GO_IMAGE through scripts/dockerfile-arg.sh; scripts/check-toolchain.py
# keeps the tags equal to go.mod and frontend/.node-version.
ARG GO_IMAGE=golang:1.27.1-trixie@sha256:8f58fd67ea075142d947a60e0caa4317746a55118d312f027793d382c7741734
ARG NODE_IMAGE=node:24.19.0-trixie-slim@sha256:ab3eebe934147fee049b5eb83c570f68c849a13c930bdfa482de99fcdfa3b3de

FROM ${NODE_IMAGE} AS frontend
WORKDIR /frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM ${GO_IMAGE} AS build
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /internal/httpserver/web ./internal/httpserver/web
RUN sh scripts/build-binary.sh /redapp "$VERSION" "$REVISION" \
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
CMD ["serve"]

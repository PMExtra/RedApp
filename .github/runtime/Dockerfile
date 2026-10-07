# Inputs are produced by the native CI build, never by a cross-compiler.
FROM scratch
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="RedApp" \
      org.opencontainers.image.source="https://github.com/PMExtra/RedApp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.licenses="MIT"
COPY redapp /redapp
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chown=65532:65532 data /var/lib/redapp
USER 65532:65532
VOLUME ["/var/lib/redapp"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/redapp", "healthcheck"]
ENTRYPOINT ["/redapp"]
CMD ["serve"]

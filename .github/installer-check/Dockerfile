# Shell-only installer validation tools. The base is pinned by digest; Ubuntu
# packages come from the pinned release's archive at build time.
#
# A transient runner DNS failure makes apt-get update only warn, so the whole
# update and install is retried; the install is what fails without indexes.
FROM ubuntu:24.04@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55
RUN for attempt in 1 2 3 4; do \
         apt-get -o Acquire::Retries=3 update \
         && apt-get -o Acquire::Retries=3 install -y --no-install-recommends \
            python3 bash curl wget patch coreutils jq tar gzip ca-certificates \
         && break; \
         [ "$attempt" -lt 4 ] || exit 1; \
         sleep $((attempt * 20)); \
       done \
    && rm -rf /var/lib/apt/lists/*

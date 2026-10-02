FROM mcr.microsoft.com/powershell:7.4-ubuntu-22.04
RUN apt-get update && apt-get install -y --no-install-recommends python3 bash curl wget patch coreutils jq tar gzip unzip ca-certificates && rm -rf /var/lib/apt/lists/*
ENV POWERSHELL_TELEMETRY_OPTOUT=1 DOTNET_CLI_TELEMETRY_OPTOUT=1
ENTRYPOINT []

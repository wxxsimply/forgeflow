ARG GO_IMAGE=golang:1.26.6-alpine
FROM ${GO_IMAGE}
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
LABEL org.opencontainers.image.title="ForgeFlow Sandbox" \
      org.opencontainers.image.description="Allowlisted ForgeFlow build and test runtimes" \
      org.opencontainers.image.source="https://github.com/wxxsimply/forgeflow" \
      org.opencontainers.image.version="${FORGEFLOW_VERSION}" \
      org.opencontainers.image.revision="${FORGEFLOW_GIT_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"
RUN apk add --no-cache git nodejs npm python3 py3-pytest cargo \
    && addgroup -S -g 10001 forgeflow \
    && adduser -S -D -H -u 10001 -G forgeflow forgeflow
USER 10001:10001
WORKDIR /workspace

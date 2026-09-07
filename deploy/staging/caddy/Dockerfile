ARG CADDY_BASE_IMAGE=caddy:2.11.4-alpine
FROM ${CADDY_BASE_IMAGE}
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
LABEL org.opencontainers.image.title="ForgeFlow Caddy" \
      org.opencontainers.image.description="ForgeFlow staging HTTPS edge proxy" \
      org.opencontainers.image.source="https://github.com/wxxsimply/forgeflow" \
      org.opencontainers.image.version="${FORGEFLOW_VERSION}" \
      org.opencontainers.image.revision="${FORGEFLOW_GIT_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"
RUN addgroup -S -g 10001 forgeflow \
    && adduser -S -D -H -u 10001 -G forgeflow forgeflow \
    && setcap -r /usr/bin/caddy \
    && chown -R 10001:10001 /data /config /etc/caddy
USER 10001:10001
EXPOSE 8080 8443 8443/udp

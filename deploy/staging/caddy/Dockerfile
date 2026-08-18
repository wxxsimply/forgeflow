ARG CADDY_BASE_IMAGE=caddy:2.11.4-alpine
FROM ${CADDY_BASE_IMAGE}
RUN addgroup -S -g 10001 forgeflow \
    && adduser -S -D -H -u 10001 -G forgeflow forgeflow \
    && setcap -r /usr/bin/caddy \
    && chown -R 10001:10001 /data /config /etc/caddy
USER 10001:10001
EXPOSE 8080 8443 8443/udp

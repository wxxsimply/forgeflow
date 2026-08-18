ARG GO_IMAGE=golang:1.26.5-alpine
FROM ${GO_IMAGE}
RUN apk add --no-cache git nodejs npm python3 py3-pytest cargo \
    && addgroup -S -g 10001 forgeflow \
    && adduser -S -D -H -u 10001 -G forgeflow forgeflow
USER 10001:10001
WORKDIR /workspace

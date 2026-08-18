ARG GO_VERSION=1.26.6
FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/forgeflow ./cmd/forgeflow
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/forgeflow-api ./cmd/forgeflow-api
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/forgeflow-worker ./cmd/forgeflow-worker

FROM alpine:3.22 AS runtime-base
RUN apk add --no-cache ca-certificates git openssh-client tzdata wget \
    && addgroup -S -g 10001 forgeflow \
    && adduser -S -D -H -u 10001 -G forgeflow forgeflow \
    && mkdir -p /var/lib/forgeflow/artifacts /var/lib/forgeflow/workspaces \
    && chown -R 10001:10001 /var/lib/forgeflow
WORKDIR /app

FROM runtime-base AS cli
COPY --from=build /out/forgeflow /usr/local/bin/forgeflow
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/forgeflow"]

FROM runtime-base AS api
COPY --from=build /out/forgeflow-api /usr/local/bin/forgeflow-api
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/forgeflow-api"]

FROM runtime-base AS worker
USER root
RUN apk add --no-cache docker-cli
COPY --from=build /out/forgeflow-worker /usr/local/bin/forgeflow-worker
USER 10001:10001
EXPOSE 9091
ENTRYPOINT ["/usr/local/bin/forgeflow-worker"]

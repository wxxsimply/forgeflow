ARG GO_VERSION=1.26.6
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
FROM golang:${GO_VERSION}-alpine AS build
ARG FORGEFLOW_GIT_COMMIT
RUN apk add --no-cache ca-certificates git
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X forgeflow/internal/buildinfo.Commit=${FORGEFLOW_GIT_COMMIT}" -o /out/forgeflow ./cmd/forgeflow
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X forgeflow/internal/buildinfo.Commit=${FORGEFLOW_GIT_COMMIT}" -o /out/forgeflow-api ./cmd/forgeflow-api
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X forgeflow/internal/buildinfo.Commit=${FORGEFLOW_GIT_COMMIT}" -o /out/forgeflow-worker ./cmd/forgeflow-worker

FROM alpine:3.22 AS runtime-base
RUN apk add --no-cache ca-certificates git openssh-client tzdata wget \
    && addgroup -S -g 10001 forgeflow \
    && adduser -S -D -H -u 10001 -G forgeflow forgeflow \
    && mkdir -p /var/lib/forgeflow/artifacts /var/lib/forgeflow/workspaces \
    && chown -R 10001:10001 /var/lib/forgeflow
WORKDIR /app

FROM runtime-base AS cli
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
LABEL org.opencontainers.image.title="ForgeFlow CLI" \
      org.opencontainers.image.description="ForgeFlow database migration and administration CLI" \
      org.opencontainers.image.source="https://github.com/wxxsimply/forgeflow" \
      org.opencontainers.image.version="${FORGEFLOW_VERSION}" \
      org.opencontainers.image.revision="${FORGEFLOW_GIT_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/forgeflow /usr/local/bin/forgeflow
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/forgeflow"]

FROM runtime-base AS api
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
LABEL org.opencontainers.image.title="ForgeFlow API" \
      org.opencontainers.image.description="ForgeFlow control-plane HTTP API" \
      org.opencontainers.image.source="https://github.com/wxxsimply/forgeflow" \
      org.opencontainers.image.version="${FORGEFLOW_VERSION}" \
      org.opencontainers.image.revision="${FORGEFLOW_GIT_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/forgeflow /usr/local/bin/forgeflow
COPY --from=build /out/forgeflow-api /usr/local/bin/forgeflow-api
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/forgeflow-api"]

FROM runtime-base AS worker
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
ARG FORGEFLOW_INSTALL_DOCKER_CLI=true
LABEL org.opencontainers.image.title="ForgeFlow Worker" \
      org.opencontainers.image.description="ForgeFlow governed workflow worker" \
      org.opencontainers.image.source="https://github.com/wxxsimply/forgeflow" \
      org.opencontainers.image.version="${FORGEFLOW_VERSION}" \
      org.opencontainers.image.revision="${FORGEFLOW_GIT_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"
USER root
RUN if [ "${FORGEFLOW_INSTALL_DOCKER_CLI}" = "true" ]; then apk add --no-cache docker-cli; fi
COPY --from=build /out/forgeflow-worker /usr/local/bin/forgeflow-worker
USER 10001:10001
EXPOSE 9091
ENTRYPOINT ["/usr/local/bin/forgeflow-worker"]

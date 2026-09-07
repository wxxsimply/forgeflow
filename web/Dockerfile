ARG NODE_VERSION=24
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
FROM node:${NODE_VERSION}-alpine AS build
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web .
COPY internal/httpapi/openapi.yaml /openapi/openapi.yaml
RUN npx openapi-typescript /openapi/openapi.yaml -o src/api/schema.d.ts \
    && npm run typecheck \
    && npm test \
    && npx vite build

FROM nginx:1.29-alpine AS web
ARG FORGEFLOW_VERSION=development
ARG FORGEFLOW_GIT_COMMIT=unknown
LABEL org.opencontainers.image.title="ForgeFlow Web" \
      org.opencontainers.image.description="ForgeFlow web console" \
      org.opencontainers.image.source="https://github.com/wxxsimply/forgeflow" \
      org.opencontainers.image.version="${FORGEFLOW_VERSION}" \
      org.opencontainers.image.revision="${FORGEFLOW_GIT_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY web/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /src/dist /usr/share/nginx/html
RUN chown -R nginx:nginx /usr/share/nginx/html
USER nginx
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

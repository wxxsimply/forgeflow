variable "REGISTRY" {
  default = "local"
}

variable "RELEASE" {
  default = "0.0.0-dev"
}

variable "GIT_COMMIT" {
  default = "0000000000000000000000000000000000000000"
}

variable "SOURCE_URL" {
  default = "https://github.com/wxxsimply/forgeflow"
}

target "_release" {
  platforms = ["linux/amd64"]
  attest = [
    "type=sbom",
    "type=provenance,mode=max"
  ]
  labels = {
    "org.opencontainers.image.source"   = SOURCE_URL
    "org.opencontainers.image.version"  = RELEASE
    "org.opencontainers.image.revision" = GIT_COMMIT
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
}

target "api" {
  inherits   = ["_release"]
  context    = "."
  dockerfile = "Dockerfile"
  target     = "api"
  args = {
    FORGEFLOW_VERSION    = RELEASE
    FORGEFLOW_GIT_COMMIT = GIT_COMMIT
  }
  tags = [
    "${REGISTRY}/forgeflow-api:${RELEASE}",
    "${REGISTRY}/forgeflow-api:sha-${GIT_COMMIT}"
  ]
}

target "worker" {
  inherits   = ["_release"]
  context    = "."
  dockerfile = "Dockerfile"
  target     = "worker"
  args = {
    FORGEFLOW_VERSION    = RELEASE
    FORGEFLOW_GIT_COMMIT = GIT_COMMIT
  }
  tags = [
    "${REGISTRY}/forgeflow-worker:${RELEASE}",
    "${REGISTRY}/forgeflow-worker:sha-${GIT_COMMIT}"
  ]
}

target "web" {
  inherits   = ["_release"]
  context    = "."
  dockerfile = "web/Dockerfile"
  target     = "web"
  args = {
    FORGEFLOW_VERSION    = RELEASE
    FORGEFLOW_GIT_COMMIT = GIT_COMMIT
  }
  tags = [
    "${REGISTRY}/forgeflow-web:${RELEASE}",
    "${REGISTRY}/forgeflow-web:sha-${GIT_COMMIT}"
  ]
}

target "caddy" {
  inherits   = ["_release"]
  context    = "deploy/staging/caddy"
  dockerfile = "Dockerfile"
  args = {
    FORGEFLOW_VERSION    = RELEASE
    FORGEFLOW_GIT_COMMIT = GIT_COMMIT
  }
  tags = [
    "${REGISTRY}/forgeflow-caddy:${RELEASE}",
    "${REGISTRY}/forgeflow-caddy:sha-${GIT_COMMIT}"
  ]
}

target "sandbox" {
  inherits   = ["_release"]
  context    = "deploy/sandbox"
  dockerfile = "Dockerfile"
  args = {
    FORGEFLOW_VERSION    = RELEASE
    FORGEFLOW_GIT_COMMIT = GIT_COMMIT
  }
  tags = [
    "${REGISTRY}/forgeflow-sandbox:${RELEASE}",
    "${REGISTRY}/forgeflow-sandbox:sha-${GIT_COMMIT}"
  ]
}

group "release" {
  targets = ["api", "worker", "web", "caddy", "sandbox"]
}

# ForgeFlow sandbox image

This image contains only the allowlisted build/test runtimes used by `run_test` and `run_static_check`. Build and push it once, record the immutable digest, then set `FORGEFLOW_SANDBOX_IMAGE=registry/repository@sha256:<digest>`.

The worker rejects tags. Task containers run as UID/GID `10001`, with no network, read-only root filesystem, dropped capabilities, `no-new-privileges`, and explicit CPU/memory/PID/tmpfs/time/output limits.

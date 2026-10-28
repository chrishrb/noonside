# syntax=docker/dockerfile:1

# The API image: the binary with its config, the database migrations and the
# Auth email templates; `serve` also runs the scheduler. The Helm chart
# (deploy/helm/noonside) runs it next to the web image (web/Dockerfile).
# Build context: backend/ (docker buildx build backend).

# STAGE 1: build the executable (cross-compiled for the target platform)
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
ARG TARGETOS TARGETARCH APP_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build/ \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags="-s -w -X github.com/chrishrb/noonside/internal/platform/errtrack.version=${APP_VERSION}" -o /app .
RUN mkdir -p /data/media

# STAGE 2: the image
FROM gcr.io/distroless/static:nonroot

USER 10000:10000

COPY --from=builder --chown=nonroot:nonroot /app /app
COPY --from=builder --chown=nonroot:nonroot /src/config /config
COPY --from=builder --chown=nonroot:nonroot /src/db/migrations /migrations
COPY --from=builder --chown=nonroot:nonroot /src/db/auth-templates /auth-templates
# Profile and system pictures: mount a writable volume here (the chart's PVC;
# docker run -v noonside-media:/data/media). Without MEDIA_DIR uploads are off.
COPY --from=builder --chown=10000:10000 /data /data
ENV MIGRATIONS_DIR=/migrations \
    AUTH_TEMPLATES_DIR=/auth-templates \
    MEDIA_DIR=/data/media

WORKDIR /
ENTRYPOINT ["/app"]
CMD ["serve"]

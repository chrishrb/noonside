# syntax=docker/dockerfile:1

# The web image: the built web app, served by nginx (nginx/). The Helm chart
# (deploy/helm/noonside) runs it next to the API image (backend/Dockerfile)
# behind one origin. Build context: web/.
#   docker buildx build web                         # the image
#   docker buildx build --target web-dist -o DIR web  # the build with source maps

# STAGE 1: build the web app (platform independent, so on the build platform)
FROM --platform=$BUILDPLATFORM node:24-alpine AS build
RUN corepack enable
WORKDIR /web
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . ./
# The commit: the app's Sentry release and the persisted cache key.
ARG APP_VERSION
# Debug IDs tie the bundles to the source maps, which the deploy uploads to
# Sentry from the web-dist stage (`mise run web:sourcemaps`). The DSN is
# runtime config (/config.js), so the image is the same everywhere.
RUN pnpm build && pnpm exec sentry-cli sourcemaps inject dist

# The build with source maps, for `docker buildx build --target web-dist -o …`.
FROM scratch AS web-dist
COPY --from=build /web/dist /

# Source maps go to Sentry, never into the image.
FROM --platform=$BUILDPLATFORM build AS files
RUN find dist -name '*.map' -delete

# STAGE 2: the image
FROM nginxinc/nginx-unprivileged:1.29-alpine
COPY --from=files /web/dist /usr/share/nginx/html
COPY nginx/*.template /etc/nginx/templates/
COPY --chmod=755 nginx/05-conf-dir.sh /docker-entrypoint.d/
# The rendered config lives in /tmp, so only /tmp needs to be writable.
USER root
RUN rm -rf /etc/nginx/conf.d && ln -s /tmp/conf.d /etc/nginx/conf.d
USER 101
# Read when the container starts (/config.js, the CSP); empty turns Sentry off.
ENV SENTRY_DSN="" \
    SENTRY_ENVIRONMENT="" \
    SENTRY_TRACES_SAMPLE_RATE="" \
    SENTRY_REPLAYS_ON_ERROR_SAMPLE_RATE="" \
    CSP_CONNECT_SRC="" \
    NGINX_ENVSUBST_OUTPUT_DIR=/tmp/conf.d
EXPOSE 8080

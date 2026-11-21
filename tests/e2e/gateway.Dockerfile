# syntax=docker/dockerfile:1.7

FROM node:24.15.0-bookworm-slim AS build

WORKDIR /app
RUN apt-get update \
    && apt-get install --yes --no-install-recommends g++ make python3 \
    && rm -rf /var/lib/apt/lists/*

COPY apps/lead-gateway/package.json apps/lead-gateway/package-lock.json ./
RUN npm ci

COPY apps/lead-gateway/tsconfig.json ./
COPY apps/lead-gateway/src ./src
RUN npm run build \
    && npm prune --omit=dev

FROM node:24.15.0-bookworm-slim AS runtime

ENV NODE_ENV=test
WORKDIR /app

COPY --chown=node:node --from=build /app/package.json ./package.json
COPY --chown=node:node --from=build /app/node_modules ./node_modules
COPY --chown=node:node --from=build /app/dist ./dist
COPY --chown=node:node tests/e2e/runner /e2e/runner

RUN install -d -o node -g node /data/gateway

USER node
CMD ["node", "dist/server.js"]

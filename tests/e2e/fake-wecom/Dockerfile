# syntax=docker/dockerfile:1.7

FROM node:24.15.0-alpine

WORKDIR /app
COPY --chown=node:node tests/e2e/fake-wecom/server.mjs ./server.mjs

USER node
EXPOSE 8088
CMD ["node", "server.mjs"]

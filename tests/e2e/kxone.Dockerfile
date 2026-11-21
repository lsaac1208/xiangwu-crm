# syntax=docker/dockerfile:1.7

FROM golang:1.25.12-bookworm AS build

WORKDIR /src/apps/kxone
COPY apps/kxone/go.mod apps/kxone/go.sum ./
RUN go mod download

COPY apps/kxone/ ./
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kxone-server ./cmd/server \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kxone-worker ./cmd/worker

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 e2e \
    && useradd --uid 1000 --gid e2e --create-home e2e \
    && install -d -o e2e -g e2e /app /data/kxone /data/kxone/cms-media

WORKDIR /app
COPY --from=build /out/kxone-server /usr/local/bin/kxone-server
COPY --from=build /out/kxone-worker /usr/local/bin/kxone-worker
COPY --from=build /src/apps/kxone/migrations /app/migrations

USER e2e
CMD ["/usr/local/bin/kxone-server"]

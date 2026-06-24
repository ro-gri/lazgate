FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /out/laz ./cmd/laz

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/laz /usr/local/bin/laz
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 10001 -d /app appuser \
    && mkdir -p /app/data /app/keys /app/.ssh \
    && chown -R appuser:appuser /app
USER appuser
ENV LAZ_ADDR=0.0.0.0:8088
ENV LAZ_NAME=Chamomile
ENV LAZ_STORAGE=sqlite
ENV LAZ_DATA=/app/data/laz.db
ENV HOME=/app
EXPOSE 8088
CMD ["laz"]

# Build and runtime Go version match go.mod (1.25.x).
FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wagering ./cmd/wagering

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=build /out/wagering /usr/local/bin/wagering
COPY migrations /migrations

ENV MIGRATIONS_PATH=/migrations

USER nobody
WORKDIR /

EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=3s --start-period=20s --retries=12 \
	CMD curl -fsS http://127.0.0.1:8080/health/live >/dev/null

ENTRYPOINT ["/usr/local/bin/wagering"]

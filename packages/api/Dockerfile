ARG GOLANG_VERSION=1.25.4
ARG ALPINE_VERSION=3.22

FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder

# Install make and CGO dependencies for JuiceFS compression (lz4, zstd) and SQLite
RUN apk add --no-cache make gcc musl-dev sqlite-dev

# Copy all go.mod and go.sum files first for dependency caching
# This ensures replace directives work correctly
WORKDIR /build

COPY ./shared/go.mod ./shared/go.sum ./shared/
COPY ./clickhouse/go.mod ./clickhouse/go.sum ./clickhouse/
COPY ./db/go.mod ./db/go.sum ./db/
COPY ./api/go.mod ./api/go.sum ./api/

# Download dependencies (replace directives now work since all directories exist)
WORKDIR /build/db
RUN go mod download

WORKDIR /build/shared
RUN go mod download

WORKDIR /build/clickhouse
RUN go mod download

WORKDIR /build/api
RUN go mod download

# Copy source code
WORKDIR /build

COPY ./clickhouse/pkg ./clickhouse/pkg
COPY ./shared/pkg ./shared/pkg
COPY ./db/client ./db/client
COPY ./db/dberrors ./db/dberrors
COPY ./db/queries ./db/queries
COPY ./db/types ./db/types

COPY ./api/internal ./api/internal
COPY ./api/main.go ./api/main.go
COPY ./api/Makefile ./api/Makefile

WORKDIR /build/api

ARG COMMIT_SHA
ARG EXPECTED_MIGRATION_TIMESTAMP
RUN --mount=type=cache,target=/root/.cache/go-build make build COMMIT_SHA=${COMMIT_SHA} EXPECTED_MIGRATION_TIMESTAMP=${EXPECTED_MIGRATION_TIMESTAMP}

RUN chmod +x /build/api/bin/api

FROM alpine:${ALPINE_VERSION}

COPY --from=builder /build/api/bin/api .

# Install sqlite3 for journal mode conversion (WAL -> DELETE after litestream restore)
RUN apk add --no-cache sqlite

# Install litestream from moru-ai fork (for volume metadata restore/replicate)
ARG LITESTREAM_VERSION=v0.5.6-moru.1
RUN wget -q -O /usr/local/bin/litestream \
    "https://github.com/moru-ai/litestream/releases/download/${LITESTREAM_VERSION}/litestream-linux" && \
    chmod +x /usr/local/bin/litestream

# Set Gin server to the production mode
ENV GIN_MODE=release

ENTRYPOINT [ "./api"]

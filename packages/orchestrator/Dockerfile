ARG GOLANG_VERSION=1.25.4

# It has to match with the host OS version (Ubuntu 22.04 = bookworm)
ARG DEBIAN_VERSION=bookworm

FROM golang:${GOLANG_VERSION}-${DEBIAN_VERSION} AS builder

# Copy all go.mod and go.sum files first for dependency caching
# This ensures replace directives work correctly
WORKDIR /build

COPY ./shared/go.mod ./shared/go.sum ./shared/
COPY ./clickhouse/go.mod ./clickhouse/go.sum ./clickhouse/
COPY ./db/go.mod ./db/go.sum ./db/
COPY ./orchestrator/go.mod ./orchestrator/go.sum ./orchestrator/

# Download dependencies (replace directives now work since all directories exist)
WORKDIR /build/db
RUN go mod download

WORKDIR /build/shared
RUN go mod download

WORKDIR /build/clickhouse
RUN go mod download

WORKDIR /build/orchestrator
RUN go mod download

# Copy source code
WORKDIR /build

COPY ./shared/pkg ./shared/pkg
COPY ./clickhouse/pkg ./clickhouse/pkg

COPY ./orchestrator/internal ./orchestrator/internal
COPY ./orchestrator/cmd ./orchestrator/cmd
COPY ./orchestrator/main.go ./orchestrator/main.go
COPY ./orchestrator/Makefile ./orchestrator/Makefile

WORKDIR /build/orchestrator

ARG COMMIT_SHA
RUN --mount=type=cache,target=/root/.cache/go-build make build-local COMMIT_SHA=${COMMIT_SHA}

FROM scratch

COPY --from=builder /build/orchestrator/bin/clean-nfs-cache .
COPY --from=builder /build/orchestrator/bin/orchestrator .

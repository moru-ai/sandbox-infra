ARG GOLANG_VERSION=1.25.4
ARG ALPINE_VERSION=3.22

FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder

RUN apk add --no-cache make

# Copy all go.mod and go.sum files first for dependency caching
# This ensures replace directives work correctly
WORKDIR /build

COPY ./shared/go.mod ./shared/go.sum ./shared/
COPY ./clickhouse/go.mod ./clickhouse/go.sum ./clickhouse/
COPY ./db/go.mod ./db/go.sum ./db/
COPY ./client-proxy/go.mod ./client-proxy/go.sum ./client-proxy/

# Download dependencies (replace directives now work since all directories exist)
WORKDIR /build/db
RUN go mod download

WORKDIR /build/shared
RUN go mod download

WORKDIR /build/clickhouse
RUN go mod download

WORKDIR /build/client-proxy
RUN go mod download

# Copy source code
WORKDIR /build

COPY ./shared/pkg ./shared/pkg

COPY ./clickhouse/pkg ./clickhouse/pkg

COPY ./client-proxy/internal ./client-proxy/internal
COPY ./client-proxy/main.go ./client-proxy/main.go
COPY ./client-proxy/Makefile ./client-proxy/Makefile

WORKDIR /build/client-proxy

ARG COMMIT_SHA
RUN --mount=type=cache,target=/root/.cache/go-build make build COMMIT_SHA=${COMMIT_SHA}
RUN chmod +x /build/client-proxy/bin/client-proxy

FROM alpine:${ALPINE_VERSION}

COPY --from=builder /build/client-proxy/bin/client-proxy .

ENTRYPOINT ["./client-proxy"]

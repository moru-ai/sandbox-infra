ARG GOLANG_VERSION=1.25.4
ARG ALPINE_VERSION=3.22

FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder

RUN apk add --no-cache make

# Copy all go.mod and go.sum files first for dependency caching
# This ensures replace directives work correctly
WORKDIR /build

COPY ./shared/go.mod ./shared/go.sum ./shared/
COPY ./db/go.mod ./db/go.sum ./db/
COPY ./docker-reverse-proxy/go.mod ./docker-reverse-proxy/go.sum ./docker-reverse-proxy/

# Download dependencies (replace directives now work since all directories exist)
WORKDIR /build/shared
RUN go mod download

WORKDIR /build/db
RUN go mod download

WORKDIR /build/docker-reverse-proxy
RUN go mod download

# Copy source code
WORKDIR /build

COPY ./shared/pkg ./shared/pkg
COPY ./db/client ./db/client
COPY ./db/dberrors ./db/dberrors
COPY ./db/queries ./db/queries
COPY ./db/types ./db/types

COPY ./docker-reverse-proxy/internal ./docker-reverse-proxy/internal
COPY ./docker-reverse-proxy/main.go ./docker-reverse-proxy/main.go
COPY ./docker-reverse-proxy/Makefile ./docker-reverse-proxy/Makefile

WORKDIR /build/docker-reverse-proxy

ARG COMMIT_SHA
RUN --mount=type=cache,target=/root/.cache/go-build make build COMMIT_SHA=${COMMIT_SHA}

RUN chmod +x /build/docker-reverse-proxy/bin/docker-reverse-proxy

FROM alpine:${ALPINE_VERSION}

COPY --from=builder /build/docker-reverse-proxy/bin/docker-reverse-proxy .

ENTRYPOINT ["./docker-reverse-proxy"]

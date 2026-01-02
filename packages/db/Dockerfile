ARG GOLANG_VERSION=1.25.4
ARG ALPINE_VERSION=3.22

# Builder stage
FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder

# Copy all go.mod and go.sum files first for dependency caching
# This ensures replace directives work correctly
WORKDIR /build

COPY ./shared/go.mod ./shared/go.sum ./shared/
COPY ./db/go.mod ./db/go.sum ./db/

# Download dependencies (replace directives now work since all directories exist)
WORKDIR /build/shared
RUN go mod download

WORKDIR /build/db
RUN go mod download

# Copy source code
WORKDIR /build

COPY ./shared/pkg ./shared/pkg

COPY ./db/scripts/migrator.go ./db/scripts/migrator.go
COPY ./db/migrations/ ./db/migrations

WORKDIR /build/db

RUN go build -o ./bin/migrator ./scripts/migrator.go
RUN chmod +x ./bin/migrator

FROM alpine:${ALPINE_VERSION}

COPY --from=builder /build/db/bin/migrator .
COPY --from=builder  /build/db/migrations ./migrations

ENTRYPOINT ["./migrator"]

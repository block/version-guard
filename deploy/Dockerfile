FROM golang:1.24.2-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /version-guard ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 version-guard
USER version-guard
COPY --from=builder /version-guard /app/version-guard
EXPOSE 8080
ENTRYPOINT ["/app/version-guard"]

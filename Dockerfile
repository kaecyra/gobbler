# syntax=docker/dockerfile:1

# Go toolchain and Alpine versions: golang:1.26.4 matches go.mod; alpine 3.24.2
# is the release that golang's alpine3.24 variant is built on. Tags and digests
# were looked up from Docker Hub; refresh both together when bumping.
FROM golang:1.26.4-alpine3.24@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Only the application is copied in. e2e/ is test tooling and never enters the
# image (ADR-00013); .dockerignore excludes it as well. Generated templ files
# are committed, so no generation step runs here.
COPY cmd ./cmd
COPY internal ./internal

# Pure-Go SQLite driver: no cgo, so no C toolchain in this stage (ADR-00002).
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gobbler ./cmd/gobbler

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# tesseract is the local OCR engine (ADR-00006); ca-certificates is needed for
# outbound HTTPS (recipe sites, LLM and S3 APIs).
RUN apk add --no-cache ca-certificates tesseract-ocr tesseract-ocr-data-eng \
    && addgroup -g 10001 -S gobbler \
    && adduser -u 10001 -S -G gobbler -h /data -s /sbin/nologin gobbler \
    && mkdir -p /data \
    && chown gobbler:gobbler /data

COPY --from=build /out/gobbler /usr/local/bin/gobbler

# A named volume mounted at /data inherits this ownership on first use.
ENV GOBBLER_DATA_DIR=/data
VOLUME /data
USER gobbler
WORKDIR /data
EXPOSE 8080

ENTRYPOINT ["gobbler", "serve"]

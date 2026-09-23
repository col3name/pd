# syntax=docker/dockerfile:1
# Default build: CGO_ENABLED=0 (no NER). To enable NER, build with:
#   docker build --build-arg WITH_NER=1 -t pii-module-v2 .
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY openapi.go process_api.yaml ./
COPY cmd ./cmd
COPY internal ./internal

# NER requires CGO + ONNX Runtime. Build with --build-arg WITH_NER=1 to enable.
ARG WITH_NER=0
RUN if [ "$WITH_NER" = "1" ]; then \
      apk add --no-cache gcc musl-dev make; \
      CGO_ENABLED=1 go build -ldflags="-s -w" -o /out/server ./cmd/server; \
    else \
      CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server ./cmd/server; \
    fi

FROM alpine:3.20
RUN adduser -D -u 10001 app
WORKDIR /srv
USER app
COPY --from=builder /out/server /server
COPY configs ./configs
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s \
  CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["/server"]
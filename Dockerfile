# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.* ./
COPY cmd ./cmd
COPY internal ./internal

RUN go test ./...
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/canary402 ./cmd/canary402

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && \
    addgroup -g 65532 canary && \
    adduser -D -H -u 65532 -G canary canary

COPY --from=build /out/canary402 /usr/local/bin/canary402
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/canary402"]

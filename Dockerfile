FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/blink-zero/proxsport/internal/version.Version=${VERSION} \
      -X github.com/blink-zero/proxsport/internal/version.Commit=${COMMIT} \
      -X github.com/blink-zero/proxsport/internal/version.Date=${BUILD_DATE}" \
    -o /out/proxsport ./cmd/proxsport

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 proxsport
USER proxsport
COPY --from=builder /out/proxsport /usr/local/bin/proxsport
EXPOSE 9221
ENTRYPOINT ["/usr/local/bin/proxsport"]

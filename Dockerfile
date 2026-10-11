# Multi-stage Dockerfile for LockForge Backend
FROM golang:alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /lockforge-server ./cmd/lockforge-server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /lockforge-cli ./cmd/lockforge-cli
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /lockforge-benchmark ./cmd/lockforge-benchmark

FROM alpine:latest
RUN apk --no-cache add ca-certificates curl

WORKDIR /root/
COPY --from=builder /lockforge-server /usr/local/bin/lockforge-server
COPY --from=builder /lockforge-cli /usr/local/bin/lockforge-cli
COPY --from=builder /lockforge-benchmark /usr/local/bin/lockforge-benchmark

EXPOSE 8080 50051

CMD ["lockforge-server"]

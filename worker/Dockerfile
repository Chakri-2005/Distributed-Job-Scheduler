# syntax=docker/dockerfile:1
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o worker .

FROM alpine:3.18
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /app/worker .
CMD ["./worker"]

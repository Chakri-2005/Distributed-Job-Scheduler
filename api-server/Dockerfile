# syntax=docker/dockerfile:1

# ─── Stage 1: Build frontend ────────────────────────────────────
FROM node:20-alpine AS frontend-builder
WORKDIR /frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# ─── Stage 2: Build Go API server ───────────────────────────────
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY api-server/go.mod api-server/go.sum ./
RUN go mod download || true
COPY api-server/ .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o api-server .

# ─── Stage 3: Final image ──────────────────────────────────────
FROM alpine:3.18
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /app/api-server .
COPY --from=frontend-builder /frontend/dist ./frontend-dist
EXPOSE 8080
CMD ["./api-server"]

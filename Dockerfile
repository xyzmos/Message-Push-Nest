# Stage 1: Build Frontend
FROM node:20-alpine AS frontend-builder

WORKDIR /web

# Copy package files first for caching
COPY web/package*.json ./
COPY web/tsconfig*.json ./
COPY web/vite.config.ts ./
COPY web/index.html ./

# Install dependencies
RUN npm install

# Copy source code
COPY web/ ./

# Build frontend
RUN npm run build

# Stage 2: Build Backend
FROM golang:1.24 AS backend-builder

WORKDIR /app

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy frontend build artifacts to the location expected by go:embed
# The embed directive in main.go is //go:embed web/dist/*
COPY --from=frontend-builder /web/dist ./web/dist

# Build the binary
# CGO_ENABLED=0 for static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o Message-Nest main.go

# Stage 3: Runtime
FROM debian:stable-slim

ENV TZ=Asia/Shanghai

RUN apt-get update \
    && apt-get install -y ca-certificates tzdata mime-support \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary
COPY --from=backend-builder /app/Message-Nest .

# Copy configuration directory (contains app.example.ini)
COPY --from=backend-builder /app/conf ./conf

# Copy other necessary files
COPY --from=backend-builder /app/LICENSE .
COPY --from=backend-builder /app/README.md .

EXPOSE 8000

CMD ["./Message-Nest"]

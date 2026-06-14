# ==========================================
# STAGE 1: Compile Go Code (Build Environment)
# ==========================================
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy module files (if available) to utilize Docker layer caching
# If go.mod does not exist yet, it will be initialized during the build
COPY go.mod* go.sum* ./
RUN if [ ! -f go.mod ]; then go mod init govodstr; fi

# Copy the source code and build a statically linked, optimized Linux binary
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o streamer main.go

# ==========================================
# STAGE 2: Minimal Runtime Image with FFmpeg
# ==========================================
FROM alpine:latest

# Install FFmpeg & FFprobe (required for video metadata and on-demand thumbnails)
RUN apk add --no-cache ffmpeg tzdata

WORKDIR /app

# Copy the compiled Go binary from the builder stage
COPY --from=builder /app/streamer .

# Expose the internal port (will be mapped via docker-compose)
EXPOSE 8080

# Container entry point execution command
CMD ["./streamer"]
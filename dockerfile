# ==========================================
# STUFE 1: Go-Code kompilieren (Build-Umgebung mit Go 1.26)
# ==========================================
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Kopiere die Mod-Dateien (falls vorhanden), um Caching zu nutzen
# Falls du noch keine go.mod hast, erzeugt das Skript eine beim Build
COPY go.mod* go.sum* ./
RUN if [ ! -f go.mod ]; then go mod init govodstr; fi

# Kopiere die main.go und baue das statische Linux-Binary
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o streamer main.go

# ==========================================
# STUFE 2: Minimales Laufzeit-Image mit FFmpeg
# ==========================================
FROM alpine:latest

# Installiere FFmpeg & FFprobe (wichtig für Metadaten und Vorschaubilder)
RUN apk add --no-cache ffmpeg tzdata

WORKDIR /app

# Kopiere das fertige Go-Binary aus der ersten Stufe
COPY --from=builder /app/streamer .

# Exponiere den Standard-Port
EXPOSE 8080

# Startbefehl des Containers
CMD ["./streamer"]
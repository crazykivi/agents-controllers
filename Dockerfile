# ---------- Стадия 1: сборка фронтенда ----------
FROM node:22-alpine AS web
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ---------- Стадия 2: сборка Go-бэкенда ----------
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server .

# ---------- Стадия 3: финальный образ (нужен python для crew-runner) ----------
FROM python:3.12-slim
RUN pip install --no-cache-dir aider-chat crewai crewai-tools
WORKDIR /app
COPY --from=build /out/server ./server
COPY --from=web /src/dist ./static
COPY runner/ ./runner
ENV ADDR=:8080 \
    DATA_DIR=/app/data \
    STATIC_DIR=/app/static \
    RUNNER_PATH=/app/runner/crew_run.py \
    PYTHON_BIN=python \
    AIDER_BIN=aider \
    GIN_MODE=release
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["./server"]

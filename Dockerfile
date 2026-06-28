FROM golang:1.25-bookworm AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 go build -o toonflow .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ffmpeg && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/toonflow /app/toonflow

EXPOSE 8080

CMD ["/app/toonflow", "--port", "8080"]

FROM golang:1.24 AS builder

WORKDIR /app
COPY . .
RUN go mod tidy
RUN go build -o guesswhat ./cmd/server

FROM debian:bookworm-slim

WORKDIR /app
COPY --from=builder /app/guesswhat .
COPY web ./web

EXPOSE 8080
CMD ["./guesswhat"]
# Dockerfile para go-pion-stream-1
FROM golang:1.25-alpine as builder
WORKDIR /app
COPY . .
RUN go build -o go-pion-stream-1 ./cmd/server

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/go-pion-stream-1 .
EXPOSE 8080
CMD ["./go-pion-stream-1"]

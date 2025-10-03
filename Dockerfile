FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o exporter .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/exporter .
COPY limacharlie.yaml org.json ./

EXPOSE 31337
CMD ["./exporter", "limacharlie.yaml", "org.json"]

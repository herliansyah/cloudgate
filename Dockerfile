# ponytail: multi-stage build using alpine for minimal size and security
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build static binary with embedded web assets
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/cloudgate ./cmd/cloudgate

# Minimal runtime image
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /bin/cloudgate /usr/local/bin/cloudgate

ENV CLOUDGATE_CONFIG_DIR=/data
VOLUME ["/data"]
EXPOSE 5210

ENTRYPOINT ["cloudgate"]
CMD ["serve", "--host", "0.0.0.0", "--port", "5210"]

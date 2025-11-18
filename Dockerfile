FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway .

# -----------------------------------------------------------
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S gateway && adduser -S gateway -G gateway

COPY --from=builder /gateway /usr/local/bin/gateway
COPY config.yaml /etc/gateway/config.yaml

USER gateway
EXPOSE 8080

ENTRYPOINT ["gateway"]
CMD ["-config", "/etc/gateway/config.yaml"]

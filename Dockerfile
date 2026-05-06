FROM golang:1-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /recording-bot ./cmd/bot/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /recording-bot /usr/local/bin/recording-bot
ENTRYPOINT ["recording-bot"]
CMD ["-config", "/config/config.yaml"]

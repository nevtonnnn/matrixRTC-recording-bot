FROM golang:1-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -tags goolm -o /recording-bot ./cmd/bot/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /recording-bot /usr/local/bin/recording-bot
ENTRYPOINT ["recording-bot"]
CMD ["-config", "/config/config.yaml"]

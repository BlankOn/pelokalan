# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Final stage
FROM alpine:3.19

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -g '' appuser

COPY --from=builder /app/server /app/server
COPY --from=builder /app/internal/templates /app/internal/templates
COPY --from=builder /app/static /app/static
COPY --from=builder /app/migrations /app/migrations

RUN chown -R appuser:appuser /app
USER appuser

ENV SERVER_HOST=0.0.0.0
EXPOSE 8080

CMD ["/app/server"]

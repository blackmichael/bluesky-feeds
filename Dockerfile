# Build stage
FROM golang:1.25-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /app

# Install migrate tool
RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /app/server ./cmd/server

# Runtime stage
FROM alpine:3.21

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/server .
COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
COPY migrations ./migrations

EXPOSE 3000

ENTRYPOINT ["/app/server"]

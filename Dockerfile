FROM golang:1.26-alpine AS builder

# Install dependencies needed for CGO if needed, though we compile with CGO_ENABLED=0
RUN apk add --no-cache gcc musl-dev tzdata

WORKDIR /app

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build statically with multi-arch support
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o bot-camomila ./cmd/bot

# Final minimal stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/bot-camomila /app/

ENTRYPOINT ["/app/bot-camomila"]

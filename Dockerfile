# Build stage
FROM golang:1.26-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS builder

ARG VERSION=dev

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/KilimcininKorOglu/M365Bridge/pkg/models.Version=${VERSION}" -o bin/m365-bridge ./cmd/cli

# Manual extension-packaging stage used by the Compose extension-builder service.
FROM node:24-alpine AS extension-builder

WORKDIR /app

COPY extension ./extension

ENTRYPOINT ["node", "extension/package.js"]

# Runtime stage
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates git go tzdata

WORKDIR /app

COPY --from=builder /build/bin/m365-bridge ./bin/m365-bridge

# Data directory for tokens, cache, setup.json
RUN mkdir -p data/tokens

EXPOSE 8000

ENTRYPOINT ["./bin/m365-bridge"]
CMD ["serve", "--port", "8000"]

##################################
# Stage 0: Build frontend module
##################################

FROM node:20-alpine AS frontend-builder
RUN npm install -g pnpm@9
WORKDIR /frontend
COPY frontend/package.json frontend/pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install
COPY frontend/ .
RUN pnpm build

##################################
# Stage 1: Build Go executable
##################################

FROM golang:1.25-alpine AS builder

ARG APP_VERSION=1.0.0

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git make curl

# buf for proto descriptor generation
RUN curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-$(uname -s)-$(uname -m)" -o /usr/local/bin/buf && \
    chmod +x /usr/local/bin/buf

WORKDIR /src

# Pull in the sibling go-tangra-common via a BuildKit named context
# (declared in docker-compose.yaml as `additional_contexts: common:
# ../go-tangra-common`). Required because go.mod has a temporary
# `replace ../go-tangra-common` while the registration-rework branch
# is in flight — without this COPY the Go module download below fails
# trying to resolve the replace target.
COPY --from=common . /go-tangra-common/

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .

# Refresh embedded proto descriptor.
RUN buf build -o cmd/server/assets/descriptor.bin

# Drop in the federated frontend remote so go:embed picks it up.
RUN rm -rf cmd/server/assets/frontend-dist
COPY --from=frontend-builder /frontend/dist cmd/server/assets/frontend-dist/

RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    go build -ldflags "-X main.version=${APP_VERSION} -s -w" \
    -o /src/bin/asterisk-server \
    ./cmd/server

##################################
# Stage 2: Runtime image
##################################

FROM alpine:3.20

ARG APP_VERSION=1.0.0

RUN apk --no-cache add ca-certificates tzdata

ENV TZ=UTC
ENV GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn

WORKDIR /app

COPY --from=builder /src/bin/asterisk-server /app/bin/asterisk-server
COPY --from=builder /src/configs/ /app/configs/

RUN addgroup -g 1000 asterisk && \
    adduser -D -u 1000 -G asterisk asterisk && \
    mkdir -p /app/certs && chown -R asterisk:asterisk /app

USER asterisk:asterisk

EXPOSE 9800 9801

CMD ["/app/bin/asterisk-server", "-c", "/app/configs"]

LABEL org.opencontainers.image.title="Asterisk Service" \
      org.opencontainers.image.description="FreePBX call detail records and statistics" \
      org.opencontainers.image.version="${APP_VERSION}"

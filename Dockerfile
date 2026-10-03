# syntax=docker/dockerfile:1.7
FROM node:22-bookworm-slim AS ui
WORKDIR /src/ui
COPY ui/package*.json ui/.npmrc ./
RUN --mount=type=secret,id=npm_token NODE_AUTH_TOKEN="$(cat /run/secrets/npm_token)" npm ci
COPY ui/ ./
COPY api/ /src/api/
RUN npm run gen:api && npm run build
FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -tags ui -o /asterisksvc ./cmd/asterisksvc
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /asterisksvc /asterisksvc
COPY configs/dev.yaml /etc/asterisk-module/config.yaml
COPY deploy/policy.yaml /etc/asterisk-module/policy.yaml
WORKDIR /etc/asterisk-module
USER nonroot:nonroot
ENTRYPOINT ["/asterisksvc"]
CMD ["-config", "/etc/asterisk-module/config.yaml"]

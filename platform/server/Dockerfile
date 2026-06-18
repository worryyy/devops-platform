# syntax=docker/dockerfile:1

FROM node:24-bookworm-slim AS web-build

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web ./
RUN npm run build

FROM golang:1.26 AS build

WORKDIR /src/platform/server

COPY platform/server/go.mod platform/server/go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn
RUN go mod download

COPY platform/server ./
COPY --from=web-build /src/platform/server/internal/app/frontend/dist ./internal/app/frontend/dist

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/platform-server \
    ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/platform-server /usr/local/bin/platform-server

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/platform-server"]

# syntax=docker/dockerfile:1

FROM golang:1.26 AS build

ARG TARGETARCH=amd64

WORKDIR /src/platform/server

COPY platform/server/go.mod platform/server/go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn
RUN go mod download

COPY platform/server ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
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

# syntax=docker/dockerfile:1.7

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

ARG VERSION=dev
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    mkdir -p /out \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/busycal .

FROM alpine:3 AS final
LABEL org.opencontainers.image.source https://github.com/banshee-lab/busycal

COPY --from=build /out/busycal /usr/local/bin/busycal
RUN chmod +x /usr/local/bin/busycal

USER 10001:10001

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/busycal"]
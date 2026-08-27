# syntax=docker/dockerfile:1

FROM golang:1.24.2-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/new-yokosuka-server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S new-yokosuka \
    && adduser -S -G new-yokosuka new-yokosuka \
    && mkdir -p /var/lib/new-yokosuka/activity \
    && chown -R new-yokosuka:new-yokosuka /var/lib/new-yokosuka

COPY --from=build --chown=new-yokosuka:new-yokosuka \
    /out/new-yokosuka-server /usr/local/bin/new-yokosuka-server

USER new-yokosuka
WORKDIR /var/lib/new-yokosuka
EXPOSE 8080

ENTRYPOINT ["new-yokosuka-server"]

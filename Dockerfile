FROM golang:1 AS build
ENV GOTOOLCHAIN=auto CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /botboard .

FROM alpine:3
RUN apk add --no-cache ca-certificates
COPY --from=build /botboard /usr/local/bin/botboard
# Userspace networking (tsnet): no tailscaled and no TUN device. Configure with env vars:
#   TS_AUTHKEY      pre-auth key for the first registration (tailscale or headscale)
#   TS_CONTROL_URL  coordination server, e.g. https://headscale.example.com (default: Tailscale)
#   TS_HOSTNAME     node name on the tailnet (default: botboard)
#   REDIS_URL       keep node state in Redis (shared by many apps); else it is kept in TS_STATE_DIR
#   REDIS_KEY       Redis hash key for the state (default: <FLY_APP_NAME or TS_HOSTNAME>:tsnet)
ENV TS_STATE_DIR=/var/lib/botboard
RUN mkdir -p /var/lib/botboard
ENTRYPOINT ["botboard"]

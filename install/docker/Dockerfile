# v2rayA from source, ready to run.
#
# Build (from the repository root):
#   docker build -f install/docker/Dockerfile \
#     --build-arg VERSION=$(git describe --tags --always --dirty) \
#     -t ghcr.io/<owner>/<repo>:latest .
# Push:
#   docker push ghcr.io/<owner>/<repo>:latest
# Run:
#   docker run -d --restart always --privileged --network host \
#     --name v2raya -v /etc/v2raya:/etc/v2raya \
#     ghcr.io/<owner>/<repo>:latest
# Then open http://localhost:2017 and register the admin account.
#
# --privileged and --network host are only needed for the transparent
# proxy (iptables/nftables). For plain SOCKS/HTTP inbounds add
# -p 2017:2017 -p 20170:20170 instead.
# Missing geoip.dat/geosite.dat are downloaded by the service itself on
# the first start, so the image ships without them.

# ---- web interface -------------------------------------------------------
FROM node:24 AS web
WORKDIR /repo/gui
COPY gui/package.json gui/yarn.lock ./
RUN corepack enable >/dev/null 2>&1; yarn install --frozen-lockfile
COPY gui/ ./
RUN yarn build

# ---- Go binaries ----------------------------------------------------------
FROM golang:1.26 AS go
ARG VERSION=dev
WORKDIR /repo
COPY core/ ./core/
COPY service/ ./service/
COPY --from=web /repo/web ./web
RUN mkdir -p service/server/router/web && cp -a web/. service/server/router/web/
RUN git config --global --add safe.directory /repo; \
    cd core && CGO_ENABLED=0 go build -buildvcs=false -trimpath \
      -o /out/v2raya_core -ldflags="-X main.Version=$VERSION -s -w" ./main && \
    cd ../service && CGO_ENABLED=0 go build -buildvcs=false -trimpath \
      -o /out/v2raya -ldflags="-X github.com/v2rayA/v2rayA/conf.Version=$VERSION -s -w" .

# ---- runtime ----------------------------------------------------------------
FROM alpine:latest
COPY install/docker/iptables.sh install/docker/ip6tables.sh /tmp/
RUN apk add --no-cache iptables iptables-legacy nftables tzdata iproute2 ca-certificates \
    && install /tmp/iptables.sh /usr/local/bin/iptables \
    && install /tmp/ip6tables.sh /usr/local/bin/ip6tables \
    && install /tmp/iptables.sh /usr/local/bin/iptables-nft \
    && install /tmp/ip6tables.sh /usr/local/bin/ip6tables-nft \
    && install /tmp/iptables.sh /usr/local/bin/iptables-legacy \
    && install /tmp/ip6tables.sh /usr/local/bin/ip6tables-legacy \
    && rm /tmp/iptables.sh /tmp/ip6tables.sh
COPY --from=go /out/v2raya /out/v2raya_core /usr/bin/
EXPOSE 2017
VOLUME /etc/v2raya
ENTRYPOINT ["v2raya"]

# v2rayA from source, ready to run.
#
# Build (from the repository root):
#   docker build -f install/docker/Dockerfile \
#     --build-arg VERSION=$(git describe --tags --always --dirty) \
#     -t ghcr.io/<owner>/<repo>:latest .
# Push:
#   docker push ghcr.io/<owner>/<repo>:latest
# Run the transparent proxy without --privileged:
#   docker run -d --restart always --network host \
#     --cap-drop ALL --cap-add NET_ADMIN --cap-add NET_RAW \
#     --device /dev/net/tun --security-opt no-new-privileges \
#     --name v2raya -v /etc/v2raya:/etc/v2raya \
#     ghcr.io/<owner>/<repo>:latest
# Then open http://localhost:2017 and register the admin account.
#
# For plain SOCKS/HTTP inbounds, drop the capabilities and the device and
# publish -p 2017:2017 -p 20170:20170 instead. The container runs as root
# because iptables and TUN need it; the capabilities above are the least it
# can do the job with.
# Missing geoip.dat/geosite.dat are downloaded by the service itself on
# the first start, so the image ships without them.

# ---- web interface -------------------------------------------------------
FROM node:24@sha256:8fa96264f302f58bbb13aa7339c541547b56975766ce956c875da149452a3e40 AS web
WORKDIR /repo/gui
COPY gui/package.json gui/yarn.lock ./
RUN corepack enable >/dev/null 2>&1; yarn install --frozen-lockfile
COPY gui/ ./
RUN yarn build

# ---- Go binaries ----------------------------------------------------------
FROM golang:1.27@sha256:e519d3b0a5b285e5aa53ab9eef02bc6bbb9cb04608389b743a136a9ce75f0d1e AS go
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
FROM alpine:latest@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
COPY install/docker/iptables.sh install/docker/ip6tables.sh install/docker/entrypoint.sh /tmp/
RUN apk add --no-cache iptables iptables-legacy nftables tzdata iproute2 ca-certificates \
    && install /tmp/iptables.sh /usr/local/bin/iptables \
    && install /tmp/ip6tables.sh /usr/local/bin/ip6tables \
    && install /tmp/iptables.sh /usr/local/bin/iptables-nft \
    && install /tmp/ip6tables.sh /usr/local/bin/ip6tables-nft \
    && install /tmp/iptables.sh /usr/local/bin/iptables-legacy \
    && install /tmp/ip6tables.sh /usr/local/bin/ip6tables-legacy \
    && install -m 0755 /tmp/entrypoint.sh /usr/local/bin/docker-entrypoint.sh \
    && rm /tmp/iptables.sh /tmp/ip6tables.sh /tmp/entrypoint.sh \
    && install -d -m 0750 /etc/v2raya
COPY --from=go /out/v2raya /out/v2raya_core /usr/bin/
LABEL org.opencontainers.image.title="v2rayA" \
      org.opencontainers.image.description="A web GUI client of Project V with a transparent proxy" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/AnotherNot/v2rayA"
EXPOSE 2017
VOLUME /etc/v2raya
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:2017/api/version || exit 1
# The binary defaults to loopback; a container publishes its port, so the
# image explicitly listens on all interfaces and relies on the port mapping
# or the host firewall for access control.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["v2raya", "--address", "0.0.0.0:2017"]

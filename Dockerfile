FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION}" -o pantau .

FROM alpine:latest AS infracost
ARG TARGETARCH
RUN apk --no-cache add ca-certificates \
 && wget -qO- "https://github.com/infracost/infracost/releases/latest/download/infracost-linux-${TARGETARCH}.tar.gz" | tar -xz -C /tmp \
 && install -m 0755 "/tmp/infracost-linux-${TARGETARCH}" /usr/local/bin/infracost

FROM alpine:latest AS terraform
ARG TARGETARCH
ARG TERRAFORM_VERSION=1.15.9
RUN apk --no-cache add ca-certificates unzip \
 && cd /tmp \
 && wget -q "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip" \
 && wget -q "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_SHA256SUMS" \
 && grep " terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip\$" "terraform_${TERRAFORM_VERSION}_SHA256SUMS" | sha256sum -c - \
 && unzip -q "terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip" terraform -d /usr/local/bin

# Teleport tsh client (Teleport profiles). The tarball is checked against the .sha256 that cdn.teleport.dev
# publishes beside it, and only teleport/tsh is taken out of the archive. kubectl is already in the image (apk).
# Pin TSH_VERSION to a stable release from https://github.com/gravitational/teleport/releases.
FROM alpine:latest AS teleport
ARG TARGETARCH
ARG TSH_VERSION=18.11.10
RUN apk --no-cache add ca-certificates tar \
 && cd /tmp \
 && wget -q "https://cdn.teleport.dev/teleport-v${TSH_VERSION}-linux-${TARGETARCH}-bin.tar.gz" \
 && wget -q "https://cdn.teleport.dev/teleport-v${TSH_VERSION}-linux-${TARGETARCH}-bin.tar.gz.sha256" \
 && sha256sum -c "teleport-v${TSH_VERSION}-linux-${TARGETARCH}-bin.tar.gz.sha256" \
 && tar -xzf "teleport-v${TSH_VERSION}-linux-${TARGETARCH}-bin.tar.gz" -C /tmp teleport/tsh \
 && install -m 0755 /tmp/teleport/tsh /usr/local/bin/tsh \
 && rm -rf /tmp/teleport /tmp/teleport-v*

# The Teleport tsh release is linked against glibc, and Alpine uses musl, so tsh does not start on Alpine as
# shipped. This stage runs tsh once (proof that it works), then collects the glibc files it loads (ldd) at their
# own paths so the final stage can copy them over. The fallback to the system copy is not used.
FROM debian:stable-slim AS teleport-rt
COPY --from=teleport /usr/local/bin/tsh /usr/local/bin/tsh
RUN /usr/local/bin/tsh version --client >/dev/null \
 && ldd /usr/local/bin/tsh | awk '/=> \//{print $3} /^[[:space:]]*\//{print $1}' | while read -r f; do \
      case "$f" in \
        /lib64/*|/lib/ld-*) mkdir -p "/rootfs$(dirname "$f")" && cp -L "$f" "/rootfs$f" ;; \
        *) mkdir -p /rootfs/lib && cp -L "$f" "/rootfs/lib/$(basename "$f")" ;; \
      esac; \
    done

# Optional VPN client binaries for Settings -> Networks. WITH_NETBIRD=1 bakes the NetBird CLI in, so a
# container can run its own NetBird account (driver mode "instance") without the host's daemon.
FROM alpine:latest AS vpn
ARG TARGETARCH
ARG WITH_NETBIRD=0
ARG NETBIRD_VERSION=0.83.1
ARG WITH_NETCLIENT=0
ARG NETCLIENT_VERSION=1.7.0
RUN apk --no-cache add ca-certificates curl tar \
 && mkdir -p /vpn \
 && if [ "$WITH_NETCLIENT" = "1" ]; then \
      curl -fsSL -o /vpn/netclient "https://github.com/gravitl/netclient/releases/download/v${NETCLIENT_VERSION}/netclient-linux-${TARGETARCH}" \
      && chmod 0755 /vpn/netclient; \
    fi \
 && if [ "$WITH_NETBIRD" = "1" ]; then \
      cd /tmp \
      && curl -fsSLO "https://github.com/netbirdio/netbird/releases/download/v${NETBIRD_VERSION}/netbird_${NETBIRD_VERSION}_linux_${TARGETARCH}.tar.gz" \
      && curl -fsSLO "https://github.com/netbirdio/netbird/releases/download/v${NETBIRD_VERSION}/netbird_${NETBIRD_VERSION}_checksums.txt" \
      && grep " netbird_${NETBIRD_VERSION}_linux_${TARGETARCH}.tar.gz\$" "netbird_${NETBIRD_VERSION}_checksums.txt" | sha256sum -c - \
      && tar -xzf "netbird_${NETBIRD_VERSION}_linux_${TARGETARCH}.tar.gz" -C /vpn netbird; \
    fi

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata kubectl ansible-core openssh-client git opentofu openvpn wireguard-tools tailscale
COPY --from=vpn /vpn/ /usr/local/bin/
COPY --from=infracost /usr/local/bin/infracost /usr/local/bin/infracost
COPY --from=terraform /usr/local/bin/terraform /usr/local/bin/terraform
COPY --from=teleport-rt /rootfs/ /
COPY --from=teleport-rt /usr/local/bin/tsh /usr/local/bin/tsh
WORKDIR /app
COPY --from=builder /app/pantau /app/pantau
VOLUME ["/app/data"]
EXPOSE 8080 8081 15000-15019
ENTRYPOINT ["/app/pantau", "-db", "/app/data/pantau.db", "-port", "8080", "-listen", "0.0.0.0", "-tunnel-port", "8081", "-forward-ports", "15000-15019"]

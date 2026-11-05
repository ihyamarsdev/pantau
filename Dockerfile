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

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata kubectl ansible-core openssh-client git opentofu
COPY --from=infracost /usr/local/bin/infracost /usr/local/bin/infracost
COPY --from=terraform /usr/local/bin/terraform /usr/local/bin/terraform
WORKDIR /app
COPY --from=builder /app/pantau /app/pantau
VOLUME ["/app/data"]
EXPOSE 8080
ENTRYPOINT ["/app/pantau", "-db", "/app/data/pantau.db", "-port", "8080"]

ARG GO_IMAGE=golang:1.25-bookworm
ARG NODE_IMAGE=ubuntu:24.04
FROM ${GO_IMAGE} AS source
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY generated ./generated
COPY deploy/dev/bootstrap ./deploy/dev/bootstrap
ARG DELVE_VERSION=latest
RUN go install github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION}

FROM source AS server-build
RUN go build -gcflags='all=-N -l' -o /out/laz ./cmd/laz
RUN go build -o /out/lab-init ./deploy/dev/bootstrap

FROM debian:bookworm-slim AS bootstrap
COPY --from=server-build /out/lab-init /usr/local/bin/lab-init
ENTRYPOINT ["lab-init"]

FROM debian:bookworm-slim AS server
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates openssh-client wget \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -u 10001 appuser
COPY --from=server-build /out/laz /usr/local/bin/laz
COPY --from=source /go/bin/dlv /usr/local/bin/dlv
COPY deploy/dev/server-start.sh /usr/local/bin/server-start
RUN chmod 755 /usr/local/bin/server-start
USER appuser
WORKDIR /app
ENTRYPOINT ["server-start"]

FROM source AS hysteria-build
RUN go build -gcflags='all=-N -l' -o /out/agent ./cmd/lazgate-hysteria-agent

FROM source AS amnezia-build
RUN go build -gcflags='all=-N -l' -o /out/agent ./cmd/lazgate-amnezia-agent

FROM golang:1.25-bookworm AS awg-runtime-build
ARG AWG_GO_REF=b5928efb6ca19f0153958460c3d141f04abc5c2e
ARG AWG_TOOLS_REF=ee0f0a9aa34ff0a0da4b3433b9512781cfe02843
RUN git init /awg-go && git -C /awg-go remote add origin https://github.com/amnezia-vpn/amneziawg-go.git \
    && git -C /awg-go fetch --depth 1 origin ${AWG_GO_REF} && git -C /awg-go checkout --detach FETCH_HEAD \
    && git init /awg-tools && git -C /awg-tools remote add origin https://github.com/amnezia-vpn/amneziawg-tools.git \
    && git -C /awg-tools fetch --depth 1 origin ${AWG_TOOLS_REF} && git -C /awg-tools checkout --detach FETCH_HEAD
WORKDIR /awg-go
RUN CGO_ENABLED=0 go build -o /out/amneziawg-go . \
    && make -C /awg-tools/src \
    && cp /awg-tools/src/wg /out/awg \
    && cp /awg-tools/src/wg-quick/linux.bash /out/awg-quick

FROM ${NODE_IMAGE} AS node
ENV DEBIAN_FRONTEND=noninteractive container=docker
RUN apt-get update && apt-get install -y --no-install-recommends \
    systemd systemd-sysv dbus openssh-server sudo ca-certificates curl iproute2 iptables openssl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 10001 lazgate \
    && useradd -m -u 10001 -g lazgate -s /bin/bash h2-laz-agent \
    && find /etc/ssh -name 'ssh_host_*' -type f -delete \
    && mkdir -p /run/sshd /etc/ssh/sshd_config.d /var/lib/lazgate-agent \
    && chown h2-laz-agent:lazgate /var/lib/lazgate-agent \
    && printf 'PermitRootLogin yes\nPasswordAuthentication yes\n' > /etc/ssh/sshd_config.d/00-lab.conf
COPY --from=source /go/bin/dlv /usr/local/bin/dlv
COPY deploy/dev/node-prepare.sh /usr/local/sbin/lab-node-prepare
COPY deploy/dev/agent-start.sh /usr/local/bin/agent-start
COPY deploy/dev/systemd/lab-prepare.service deploy/dev/systemd/lazgate-agent.service /etc/systemd/system/
RUN chmod 755 /usr/local/sbin/lab-node-prepare /usr/local/bin/agent-start \
    && systemctl enable ssh lab-prepare lazgate-agent
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/sbin/init"]

FROM node AS hysteria
COPY deploy/dev/systemd/hysteria-server.service /etc/systemd/system/
ARG TARGETARCH
ARG HYSTERIA_VERSION=2.13.0
RUN arch="${TARGETARCH:-$(dpkg --print-architecture)}" \
    && curl -fSL --retry 3 "https://github.com/apernet/hysteria/releases/download/app/v${HYSTERIA_VERSION}/hysteria-linux-${arch}" -o /usr/local/bin/hysteria \
    && chmod 755 /usr/local/bin/hysteria \
    && systemctl enable hysteria-server \
    && install -d -o root -g root -m 755 /var/lib/lazgate-hysteria-files \
    && printf 'h2-laz-agent ALL=(root) NOPASSWD: /usr/bin/systemctl *, /usr/bin/journalctl *, /usr/bin/cp *, /usr/bin/install *, /usr/bin/cat /etc/hysteria/*, /usr/bin/cat /lab/tls.crt, /usr/bin/mv -f /etc/hysteria/.lazgate-config-pending /etc/hysteria/config.yaml, /usr/bin/rm -f /etc/hysteria/config.yaml.lazgate-settings-previous, /usr/bin/rm -rf /var/lib/lazgate-hysteria-files/*\n' > /etc/sudoers.d/lab-agent \
    && chmod 440 /etc/sudoers.d/lab-agent
COPY --from=hysteria-build /out/agent /usr/local/bin/lazgate-agent

FROM node AS amnezia
COPY deploy/dev/systemd/lab-awg.service /etc/systemd/system/
RUN usermod -l awg-laz-agent -d /home/awg-laz-agent -m h2-laz-agent \
    && mkdir -p /usr/local/lib/lazgate-dev \
    && mkdir -p /etc/systemd/system/lazgate-agent.service.d \
    && printf '[Unit]\nRequires=lab-awg.service\n[Service]\nUser=root\nGroup=root\n' > /etc/systemd/system/lazgate-agent.service.d/awg.conf \
    && printf 'awg-laz-agent ALL=(root) NOPASSWD: /usr/local/bin/awg *, /usr/bin/systemctl *, /usr/bin/journalctl *, /usr/sbin/ip address replace *\n' > /etc/sudoers.d/lab-awg-agent \
    && chmod 440 /etc/sudoers.d/lab-awg-agent \
    && systemctl enable lab-awg
COPY deploy/dev/awg-start.sh /usr/local/lib/lazgate-dev/awg-start.sh
COPY --from=awg-runtime-build /out/ /usr/local/bin/
RUN chmod 755 /usr/local/lib/lazgate-dev/awg-start.sh /usr/local/bin/awg-quick
COPY --from=amnezia-build /out/agent /usr/local/bin/lazgate-agent

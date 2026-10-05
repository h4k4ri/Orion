FROM debian:trixie

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    jq \
    lvm2 \
    libvirt-clients \
    libvirt-daemon-driver-qemu \
    libvirt-daemon-system \
    libvirt0 \
    nats-server \
    openvswitch-switch \
    postgresql-common \
    procps \
    qemu-system-x86 \
    qemu-utils \
    tini \
 && install -d /usr/share/postgresql-common/pgdg \
 && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
 && . /etc/os-release \
 && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt ${VERSION_CODENAME}-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends postgresql-18 \
 && rm -rf /var/lib/apt/lists/*

COPY out/*.deb /opt/orion/
COPY e2e/container-smoke.sh /usr/local/bin/orion-deb-e2e

RUN chmod 0755 /usr/local/bin/orion-deb-e2e

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/orion-deb-e2e"]

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    zfsutils-linux \
    kmod \
    curl \
    golang-go \
    && ln -s /usr/bin/lsblk /usr/bin/blockdev \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && rm -rf /var/cache/apt/archives/*

ENV PATH=$PATH:/usr/local/go/bin
ENV GOPATH=/root/go
ENV PATH=$PATH:$GOPATH/bin

WORKDIR /plugin

COPY sdk/go /plugin/sdk/go
COPY plugins/zfs /plugin/plugins/zfs

RUN rm -f /plugin/plugins/zfs/go.mod /plugin/plugins/zfs/go.sum && \
    cd /plugin/plugins/zfs && \
    go mod init github.com/horizon/orion/plugins/zfs && \
    go mod edit -replace github.com/horizon/orion/sdk/go=/plugin/sdk/go && \
    go mod tidy && \
    go build -o /plugin/orion-zfs-plugin ./cmd

RUN chmod +x /plugin/orion-zfs-plugin

ENV ORION_PLUGIN_ENDPOINT=:50051

EXPOSE 50051

CMD ["/plugin/orion-zfs-plugin"]

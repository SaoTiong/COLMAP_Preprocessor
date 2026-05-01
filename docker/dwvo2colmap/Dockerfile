ARG UBUNTU_VERSION=24.04
ARG GO_VERSION=1.26.1

FROM ubuntu:${UBUNTU_VERSION}

ARG GO_VERSION

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        libgl1 \
        libx11-6 \
        libx11-xcb1 \
        libxcursor1 \
        libxi6 \
        libxinerama1 \
        libxrandr2 \
        libxfixes3 \
        libxxf86vm1 \
        libgl1-mesa-dev \
        xorg-dev && \
    rm -rf /var/lib/apt/lists/*

RUN rm -rf /usr/local/go && \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm -f /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

# WORKDIR /workspace

CMD ["/bin/bash"]

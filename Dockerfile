ARG BDINFO_REPO=https://github.com/mirrorb/BDInfoCLI.git
ARG BDINFO_REF=master
ARG BDINFO_CSPROJ=BDInfo/BDInfo.csproj
ARG GO_VERSION=1.26.1
ARG APP_VERSION=dev
ARG DEBIAN_RELEASE=bookworm-slim
ARG FFMPEG_URL_AMD64=https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-n8.1-latest-linux64-gpl-shared-8.1.tar.xz
ARG FFMPEG_URL_ARM64=https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-n8.1-latest-linuxarm64-gpl-shared-8.1.tar.xz

# 构建 WebUI
FROM --platform=$BUILDPLATFORM node:20-alpine AS webui
ARG APP_VERSION=dev
WORKDIR /app
COPY webui/package.json ./
RUN npm install --no-audit --no-fund
COPY webui .
ENV VITE_APP_VERSION=$APP_VERSION
RUN npm run build

# 构建 Go 后端
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG APP_VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd ./cmd
COPY internal ./internal
COPY --from=webui /app/dist ./webui/dist
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w -X minfo/internal/version.Version=${APP_VERSION}" -o /out/minfo ./cmd/minfo

# 下载 FFmpeg 8.1 shared 构建
FROM --platform=$BUILDPLATFORM debian:${DEBIAN_RELEASE} AS ffmpeg-dist
ARG TARGETARCH
ARG FFMPEG_URL_AMD64
ARG FFMPEG_URL_ARM64
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    findutils \
    xz-utils \
    && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    case "$TARGETARCH" in \
        amd64) ffmpeg_url="$FFMPEG_URL_AMD64" ;; \
        arm64) ffmpeg_url="$FFMPEG_URL_ARM64" ;; \
        *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    mkdir -p /tmp/ffmpeg-root /out; \
    curl -fsSL "$ffmpeg_url" -o /tmp/ffmpeg.tar.xz; \
    tar -xJf /tmp/ffmpeg.tar.xz -C /tmp/ffmpeg-root; \
    extracted_dir="$(find /tmp/ffmpeg-root -mindepth 1 -maxdepth 1 -type d | head -n 1)"; \
    if [ -z "$extracted_dir" ]; then \
        echo "ffmpeg archive layout unexpected" >&2; exit 1; \
    fi; \
    mv "$extracted_dir" /out/ffmpeg; \
    mkdir -p /out/ffmpeg-runtime/usr/bin /out/ffmpeg-runtime/usr/lib; \
    install -m 0755 /out/ffmpeg/bin/ffmpeg /out/ffmpeg-runtime/usr/bin/ffmpeg; \
    install -m 0755 /out/ffmpeg/bin/ffprobe /out/ffmpeg-runtime/usr/bin/ffprobe; \
    find /out/ffmpeg/lib -mindepth 1 -maxdepth 1 \( -type f -o -type l \) \
        \( -name '*.so' -o -name '*.so.*' \) \
        -exec cp -a {} /out/ffmpeg-runtime/usr/lib/ \; && \
    test -x /out/ffmpeg/bin/ffmpeg; \
    test -x /out/ffmpeg/bin/ffprobe

# 构建 BDInfo (.NET)
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0 AS bdinfo-build
ARG BDINFO_REPO
ARG BDINFO_REF
ARG BDINFO_CSPROJ
ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "$BDINFO_REF" "$BDINFO_REPO" /src/bdinfo
WORKDIR /src/bdinfo
RUN set -eux; \
    # 匹配 Debian/glibc 运行环境的 RID
    case "$TARGETARCH" in \
        amd64) rid="linux-x64" ;; \
        arm64) rid="linux-arm64" ;; \
        *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    dotnet restore "$BDINFO_CSPROJ"; \
    # 编译单文件版 (禁用 Trim 以防命令行反射报错)
    dotnet publish "$BDINFO_CSPROJ" -c Release -r "$rid" --self-contained true \
        -p:PublishSingleFile=true \
        -p:EnableCompressionInSingleFile=true \
        -p:DebugType=None \
        -p:DebugSymbols=false \
        -o /out/bdinfo; \
    # 提取生成的二进制文件
    exe=""; \
    for f in /out/bdinfo/*; do \
        [ -f "$f" ] || continue; \
        [ -x "$f" ] || continue; \
        case "${f##*.}" in \
            dll|json|pdb) continue ;; \
        esac; \
        exe="$f"; \
        break; \
    done; \
    if [ -n "$exe" ]; then \
        if [ "$exe" != "/out/bdinfo/BDInfo" ]; then \
            mv "$exe" /out/bdinfo/BDInfo; \
        fi; \
    else \
        echo "BDInfo executable not found" >&2; exit 1; \
    fi; \
    chmod +x /out/bdinfo/BDInfo; \
    find /out/bdinfo -type f \( -name '*.pdb' -o -name '*.xml' -o -name '*.dbg' \) -delete

# 构建 BD 元数据 helper
FROM debian:${DEBIAN_RELEASE} AS media-helper-build
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY tools/bdsub_probe.c ./tools/bdsub_probe.c
RUN mkdir -p /out && \
    cc -O2 -Wall -Wextra -std=c11 ./tools/bdsub_probe.c -o /out/bdsub

# 最终运行环境 (Debian)
FROM debian:${DEBIAN_RELEASE} AS runtime
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    mediainfo \
    fontconfig \
    fonts-noto-cjk \
    kmod \
    libgdiplus \
    findutils \
    util-linux \
    libstdc++6 \
    libgcc-s1 \
    tzdata \
    bash \
    jq \
    bc \
    file \
    coreutils \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ffmpeg-dist /out/ffmpeg-runtime/usr/bin/ffmpeg /out/ffmpeg-runtime/usr/bin/ffprobe /usr/bin/
COPY --from=ffmpeg-dist /out/ffmpeg-runtime/usr/lib/ /usr/lib/

RUN set -eux; \
    ldconfig; \
    printf '#!/bin/sh\nexec "$@"\n' > /usr/local/bin/sudo; \
    chmod +x /usr/local/bin/sudo

COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /out/bdinfo/BDInfo /usr/local/bin/bdinfo
COPY --from=media-helper-build /out/bdsub /usr/local/bin/bdsub

RUN chmod +x /usr/local/bin/minfo /usr/local/bin/bdinfo /usr/local/bin/bdsub /usr/bin/ffmpeg /usr/bin/ffprobe

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=28080
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

EXPOSE 28080
ENTRYPOINT ["/usr/local/bin/minfo"]

# 本地调试环境 (Go + Delve + 运行依赖)
FROM golang:${GO_VERSION}-bookworm AS debug
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    mediainfo \
    fontconfig \
    fonts-noto-cjk \
    kmod \
    libgdiplus \
    findutils \
    util-linux \
    libstdc++6 \
    libgcc-s1 \
    tzdata \
    bash \
    jq \
    bc \
    file \
    coreutils \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ffmpeg-dist /out/ffmpeg-runtime/usr/bin/ffmpeg /out/ffmpeg-runtime/usr/bin/ffprobe /usr/bin/
COPY --from=ffmpeg-dist /out/ffmpeg-runtime/usr/lib/ /usr/lib/

RUN set -eux; \
    ldconfig

RUN GOBIN=/usr/local/bin go install github.com/go-delve/delve/cmd/dlv@latest

COPY --from=runtime /usr/local/bin/bdinfo /usr/local/bin/bdinfo
COPY --from=runtime /usr/local/bin/bdsub /usr/local/bin/bdsub
COPY --from=runtime /usr/local/bin/sudo /usr/local/bin/sudo

RUN chmod +x /usr/local/bin/dlv /usr/local/bin/bdinfo /usr/local/bin/bdsub /usr/local/bin/sudo /usr/bin/ffmpeg /usr/bin/ffprobe

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=28080
ENV GOCACHE=/tmp/go-build
ENV GOROOT=/usr/local/go
ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

WORKDIR /opt/minfo
EXPOSE 28080 2345
CMD ["dlv", "dap", "--listen=:2345"]

# 默认最终镜像保持为运行环境，避免 `docker build .` 误落到 debug stage。
FROM runtime AS final

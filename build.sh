#!/bin/bash
# AlertFly 编译脚本
# 用法: ./build.sh [linux|windows|all]

set -e

export PATH=$PATH:/usr/local/go/bin
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
BUILD_TIME=$(date '+%Y-%m-%d %H:%M:%S')
LDFLAGS="-X main.version=${VERSION} -X 'main.buildTime=${BUILD_TIME}'"

BUILD_DIR="build"
mkdir -p ${BUILD_DIR}

build_linux() {
    echo ">>> 编译 Linux amd64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -mod=vendor -ldflags "${LDFLAGS}" \
        -o ${BUILD_DIR}/alertfly-linux-amd64 ./cmd/alertfly/
    echo ">>> 完成: ${BUILD_DIR}/alertfly-linux-amd64"
}

build_windows() {
    echo ">>> 编译 Windows amd64 (CGO)..."
    CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
        go build -mod=vendor -ldflags "${LDFLAGS} -H=windowsgui" \
        -o ${BUILD_DIR}/alertfly-windows-amd64.exe ./cmd/alertfly/
    echo ">>> 完成: ${BUILD_DIR}/alertfly-windows-amd64.exe"
}

pack() {
    echo ">>> 打包发布..."
    VERSION_TAG=$(git describe --tags --always 2>/dev/null || echo "dev")

    # 计算 sha256 供参考（部署者需要根据实际 URL 修改 version.json）
    LINUX_SHA=$(sha256sum ${BUILD_DIR}/alertfly-linux-amd64 | awk '{print $1}')
    WIN_SHA=$(sha256sum ${BUILD_DIR}/alertfly-windows-amd64.exe | awk '{print $1}')
    echo ">>> Linux SHA256: ${LINUX_SHA}"
    echo ">>> Windows SHA256: ${WIN_SHA}"

    # 同步到本地 update-server 目录（方便本地启动更新服务测试）
    cp ${BUILD_DIR}/alertfly-linux-amd64 update-server/alertfly
    cp ${BUILD_DIR}/alertfly-windows-amd64.exe update-server/alertfly.exe
    echo ">>> 已同步二进制到本地 update-server/"

    # Linux 包
    LINUX_DIR="${BUILD_DIR}/alertfly-${VERSION_TAG}-linux-amd64"
    mkdir -p "${LINUX_DIR}/update-server"
    cp ${BUILD_DIR}/alertfly-linux-amd64 "${LINUX_DIR}/alertfly"
    cp config.yaml.example "${LINUX_DIR}/"
    cp README.md "${LINUX_DIR}/"
    cp update-server/start_server.py "${LINUX_DIR}/update-server/"
    cp update-server/version.json "${LINUX_DIR}/update-server/"
    cp update-server/README.md "${LINUX_DIR}/update-server/"
    # update-server 中也放一份二进制供更新分发
    cp ${BUILD_DIR}/alertfly-linux-amd64 "${LINUX_DIR}/update-server/alertfly"
    tar -czf "${BUILD_DIR}/alertfly-${VERSION_TAG}-linux-amd64.tar.gz" -C ${BUILD_DIR} "alertfly-${VERSION_TAG}-linux-amd64"
    rm -rf "${LINUX_DIR}"
    echo ">>> 完成: ${BUILD_DIR}/alertfly-${VERSION_TAG}-linux-amd64.tar.gz"

    # Windows 包
    WIN_DIR="${BUILD_DIR}/alertfly-${VERSION_TAG}-windows-amd64"
    mkdir -p "${WIN_DIR}/update-server"
    cp ${BUILD_DIR}/alertfly-windows-amd64.exe "${WIN_DIR}/alertfly.exe"
    cp config.yaml.example "${WIN_DIR}/"
    cp README.md "${WIN_DIR}/"
    cp update-server/start_server.py "${WIN_DIR}/update-server/"
    cp update-server/version.json "${WIN_DIR}/update-server/"
    cp update-server/README.md "${WIN_DIR}/update-server/"
    cp ${BUILD_DIR}/alertfly-windows-amd64.exe "${WIN_DIR}/update-server/alertfly.exe"
    tar -czf "${BUILD_DIR}/alertfly-${VERSION_TAG}-windows-amd64.tar.gz" -C ${BUILD_DIR} "alertfly-${VERSION_TAG}-windows-amd64"
    rm -rf "${WIN_DIR}"
    echo ">>> 完成: ${BUILD_DIR}/alertfly-${VERSION_TAG}-windows-amd64.tar.gz"
}

case "${1:-all}" in
    linux)
        build_linux
        ;;
    windows)
        build_windows
        ;;
    pack)
        build_linux
        build_windows
        pack
        ;;
    all)
        build_linux
        build_windows
        pack
        ;;
    *)
        echo "用法: $0 [linux|windows|pack|all]"
        exit 1
        ;;
esac

echo ">>> 编译完成！输出目录: ${BUILD_DIR}/"
ls -lh ${BUILD_DIR}/

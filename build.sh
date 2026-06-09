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

case "${1:-all}" in
    linux)
        build_linux
        ;;
    windows)
        build_windows
        ;;
    all)
        build_linux
        build_windows
        ;;
    *)
        echo "用法: $0 [linux|windows|all]"
        exit 1
        ;;
esac

echo ">>> 编译完成！输出目录: ${BUILD_DIR}/"
ls -lh ${BUILD_DIR}/

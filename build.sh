#!/bin/bash
# AlertFly 编译脚本
# 用法: ./build.sh [linux|windows|pack|all] [版本号]
#
# 使用 git tag 版本（默认）
#   ./build.sh all
#   ./build.sh linux
#
# 手动指定版本号
#   ./build.sh all 0.3.0
#   ./build.sh linux 0.3.0

set -e

export PATH=$PATH:/usr/local/go/bin

# 第二个参数为版本号（可选）
if [ -n "$2" ]; then
    VERSION="$2"
else
    VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
fi
# 去掉 v 前缀用于 version.json
VERSION_CLEAN=$(echo "${VERSION}" | sed 's/^[vV]//')

BUILD_TIME=$(date '+%Y-%m-%d %H:%M:%S')
LDFLAGS="-X main.version=${VERSION} -X 'main.buildTime=${BUILD_TIME}'"

BUILD_DIR="build"
VERSION_JSON="update-server/version.json"
mkdir -p ${BUILD_DIR}

# 更新 version.json 中的指定字段
# 用法: update_version_json <field> <value>
update_version_json() {
    local field="$1"
    local value="$2"
    sed -i "s/\"${field}\": *\"[^\"]*\"/\"${field}\": \"${value}\"/" "${VERSION_JSON}"
}

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

# 更新 version.json：版本号和指定平台的 sha256
# 用法: update_version_info [linux] [windows]
update_version_info() {
    # 更新 version 字段
    update_version_json "version" "${VERSION_CLEAN}"
    echo ">>> 已更新 ${VERSION_JSON}: version=${VERSION_CLEAN}"

    for platform in "$@"; do
        case "${platform}" in
            linux)
                cp ${BUILD_DIR}/alertfly-linux-amd64 update-server/alertfly
                local linux_sha=$(sha256sum ${BUILD_DIR}/alertfly-linux-amd64 | awk '{print $1}')
                update_version_json "linux_sha256" "${linux_sha}"
                echo ">>> 已更新 ${VERSION_JSON}: linux_sha256=${linux_sha}"
                ;;
            windows)
                cp ${BUILD_DIR}/alertfly-windows-amd64.exe update-server/alertfly.exe
                local win_sha=$(sha256sum ${BUILD_DIR}/alertfly-windows-amd64.exe | awk '{print $1}')
                update_version_json "windows_sha256" "${win_sha}"
                echo ">>> 已更新 ${VERSION_JSON}: windows_sha256=${win_sha}"
                ;;
        esac
    done
}

pack() {
    echo ">>> 打包发布..."

    # 计算 sha256 供参考
    LINUX_SHA=$(sha256sum ${BUILD_DIR}/alertfly-linux-amd64 | awk '{print $1}')
    WIN_SHA=$(sha256sum ${BUILD_DIR}/alertfly-windows-amd64.exe | awk '{print $1}')
    echo ">>> Linux SHA256: ${LINUX_SHA}"
    echo ">>> Windows SHA256: ${WIN_SHA}"

    # 同步到本地 update-server 目录（方便本地启动更新服务测试）
    cp ${BUILD_DIR}/alertfly-linux-amd64 update-server/alertfly
    cp ${BUILD_DIR}/alertfly-windows-amd64.exe update-server/alertfly.exe
    echo ">>> 已同步二进制到本地 update-server/"

    # Linux 包
    LINUX_DIR="${BUILD_DIR}/alertfly-${VERSION}-linux-amd64"
    mkdir -p "${LINUX_DIR}/update-server"
    cp ${BUILD_DIR}/alertfly-linux-amd64 "${LINUX_DIR}/alertfly"
    cp config.yaml.example "${LINUX_DIR}/"
    cp README.md "${LINUX_DIR}/"
    cp update-server/start_server.py "${LINUX_DIR}/update-server/"
    cp update-server/version.json "${LINUX_DIR}/update-server/"
    cp update-server/README.md "${LINUX_DIR}/update-server/"
    cp ${BUILD_DIR}/alertfly-linux-amd64 "${LINUX_DIR}/update-server/alertfly"
    tar -czf "${BUILD_DIR}/alertfly-${VERSION}-linux-amd64.tar.gz" -C ${BUILD_DIR} "alertfly-${VERSION}-linux-amd64"
    rm -rf "${LINUX_DIR}"
    echo ">>> 完成: ${BUILD_DIR}/alertfly-${VERSION}-linux-amd64.tar.gz"

    # Windows 包
    WIN_DIR="${BUILD_DIR}/alertfly-${VERSION}-windows-amd64"
    mkdir -p "${WIN_DIR}/update-server"
    cp ${BUILD_DIR}/alertfly-windows-amd64.exe "${WIN_DIR}/alertfly.exe"
    cp config.yaml.example "${WIN_DIR}/"
    cp README.md "${WIN_DIR}/"
    cp update-server/start_server.py "${WIN_DIR}/update-server/"
    cp update-server/version.json "${WIN_DIR}/update-server/"
    cp update-server/README.md "${WIN_DIR}/update-server/"
    cp ${BUILD_DIR}/alertfly-windows-amd64.exe "${WIN_DIR}/update-server/alertfly.exe"
    tar -czf "${BUILD_DIR}/alertfly-${VERSION}-windows-amd64.tar.gz" -C ${BUILD_DIR} "alertfly-${VERSION}-windows-amd64"
    rm -rf "${WIN_DIR}"
    echo ">>> 完成: ${BUILD_DIR}/alertfly-${VERSION}-windows-amd64.tar.gz"
}

case "${1:-all}" in
    linux)
        build_linux
        update_version_info linux
        ;;
    windows)
        build_windows
        update_version_info windows
        ;;
    pack)
        build_linux
        build_windows
        update_version_info linux windows
        pack
        ;;
    all)
        build_linux
        build_windows
        update_version_info linux windows
        pack
        ;;
    *)
        echo "用法: $0 [linux|windows|pack|all] [版本号]"
        exit 1
        ;;
esac

echo ">>> 编译完成！输出目录: ${BUILD_DIR}/"
ls -lh ${BUILD_DIR}/

# AlertFly 内网编译指南

## 前置条件

### Linux 编译（无 CGO）
- Go 1.17.13 已安装
- 无需额外依赖

### Windows 交叉编译（需 CGO）
- Go 1.17.13 已安装
- mingw-w64 已安装：`apt install gcc-mingw-w64-x86-64`
- 或解压工具链包：`tar -xzf mingw-w64-toolchain.tar.gz -C /`

## 编译命令

### 一键编译
```
./build.sh all
```

### 单独编译 Linux
```
./build.sh linux
```

### 单独编译 Windows
```
./build.sh windows
```

## 输出
- build/alertfly-linux-amd64
- build/alertfly-windows-amd64.exe

## 内网部署工具链包

如果目标机器没有网络，需要预先安装以下 deb 包：
- gcc-mingw-w64-x86-64
- gcc-mingw-w64-base
- binutils-mingw-w64-x86-64
- mingw-w64-common
- mingw-w64-x86-64-dev

可从 Ubuntu 20.04 mirror 下载对应 deb 文件离线安装。

也可使用已打包的工具链压缩包 `build/mingw-w64-toolchain.tar.gz`（约 69M），
解压到根目录即可：
```
sudo tar -xzf mingw-w64-toolchain.tar.gz -C /
```

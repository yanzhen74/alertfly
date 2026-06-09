# AlertFly 自更新服务

## 使用方法

1. 将新版本二进制放到本目录
2. 计算 SHA256：
   - Linux: `sha256sum alertfly`
   - Windows: `Get-FileHash alertfly.exe -Algorithm SHA256`
3. 编辑 `version.json`，更新 version、url、sha256
4. 启动服务：`python3 start_server.py`
5. 客户端配置 `updater.check_url` 指向 `http://<服务器IP>:8000/version.json`

## 注意

- url 字段要用客户端能访问的内网 IP，不要用 localhost
- 服务默认监听 0.0.0.0:8000，所有网卡都可访问
- 支持 Linux/Windows 二进制同时放在本目录

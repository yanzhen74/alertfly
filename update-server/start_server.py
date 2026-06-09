#!/usr/bin/env python3
"""AlertFly 自更新文件服务器 - 在本目录启动 HTTP 服务"""
import http.server, socketserver, os
os.chdir(os.path.dirname(os.path.abspath(__file__)))
PORT = 8000
print(f"更新服务已启动: http://0.0.0.0:{PORT}")
print(f"客户端配置 check_url: http://<本机IP>:{PORT}/version.json")
socketserver.TCPServer(("0.0.0.0", PORT), http.server.SimpleHTTPRequestHandler).serve_forever()

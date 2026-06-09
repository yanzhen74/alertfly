@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion

REM send_alert.bat — 手动发送报警消息到 AlertFly (Windows)
REM
REM 用法:
REM   交互式:     双击运行 或 send_alert.bat
REM   命令行:     send_alert.bat "标题" "内容"
REM   覆盖 level: send_alert.bat "标题" "内容" error

REM ── 脚本所在目录 ──
set "SCRIPT_DIR=%~dp0"
REM 去掉末尾反斜杠
set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"
set "CONF_FILE=%SCRIPT_DIR%\send_alert.user"

REM ── 检查配置文件 ──
if not exist "%CONF_FILE%" (
    echo [错误] 配置文件不存在: %CONF_FILE%
    echo.
    echo 请先复制模板并修改:
    echo   copy "%SCRIPT_DIR%\send_alert.user.example" "%CONF_FILE%"
    exit /b 1
)

REM ── 读取配置 ──
set "REDIS_ADDR=localhost:6379"
set "REDIS_PASSWORD="
set "REDIS_DB=0"
set "CHANNEL=alerts"
set "MODE=pubsub"
set "LEVEL=warn"
set "SUBTYPE=manual"
set "SENDER=manual"
set "MISSION="
set "SOURCE=redis"

for /f "usebackq tokens=1,* delims==" %%a in ("%CONF_FILE%") do (
    set "line=%%a"
    REM 跳过注释和空行
    if not "!line:~0,1!"=="#" (
        if not "!line!"=="" (
            set "key=%%a"
            set "val=%%b"
            REM 去除 key 前后空白
            for /f "tokens=* delims= " %%k in ("!key!") do set "key=%%k"
            if not "!key!"=="" (
                if "!key!"=="REDIS_ADDR" set "REDIS_ADDR=!val!"
                if "!key!"=="REDIS_PASSWORD" set "REDIS_PASSWORD=!val!"
                if "!key!"=="REDIS_DB" set "REDIS_DB=!val!"
                if "!key!"=="CHANNEL" set "CHANNEL=!val!"
                if "!key!"=="MODE" set "MODE=!val!"
                if "!key!"=="LEVEL" set "LEVEL=!val!"
                if "!key!"=="SUBTYPE" set "SUBTYPE=!val!"
                if "!key!"=="SENDER" set "SENDER=!val!"
                if "!key!"=="MISSION" set "MISSION=!val!"
                if "!key!"=="SOURCE" set "SOURCE=!val!"
            )
        )
    )
)

REM ── 获取输入 ──
set "TITLE="
set "CONTENT="
set "OVERRIDE_LEVEL="

if "%~2"=="" (
    if "%~1"=="" (
        REM 交互式输入
        echo === AlertFly 手动报警发送 ===
        echo.
        set /p "TITLE=请输入标题 (title): "
        set /p "CONTENT=请输入内容 (content): "
        set /p "OVERRIDE_LEVEL=覆盖级别 (level，回车使用默认 %LEVEL%): "
        echo.
    ) else (
        echo 用法: %~nx0 "标题" "内容" [level]
        echo.
        echo level 可选值: info, warn, error ^(默认: %LEVEL%^)
        exit /b 1
    )
) else (
    set "TITLE=%~1"
    set "CONTENT=%~2"
    if not "%~3"=="" set "OVERRIDE_LEVEL=%~3"
)

REM ── 验证必填字段 ──
if "!TITLE!"=="" (
    echo [错误] 标题不能为空
    exit /b 1
)
if "!CONTENT!"=="" (
    echo [错误] 内容不能为空
    exit /b 1
)

REM ── 确定 level ──
if not "!OVERRIDE_LEVEL!"=="" (
    set "FINAL_LEVEL=!OVERRIDE_LEVEL!"
) else (
    set "FINAL_LEVEL=!LEVEL!"
)

REM 验证 level 值
if not "!FINAL_LEVEL!"=="info" if not "!FINAL_LEVEL!"=="warn" if not "!FINAL_LEVEL!"=="error" (
    echo [错误] level 必须是 info, warn, error 之一，当前: !FINAL_LEVEL!
    exit /b 1
)

REM ── JSON 转义 ──
call :json_escape "!TITLE!" ESC_TITLE
call :json_escape "!CONTENT!" ESC_CONTENT
call :json_escape "!MISSION!" ESC_MISSION
call :json_escape "!SENDER!" ESC_SENDER

REM ── 组装 JSON ──
set "JSON_PAYLOAD={"source":"!SOURCE!","topic":"!CHANNEL!","level":"!FINAL_LEVEL!","subtype":"!SUBTYPE!","title":"!ESC_TITLE!","mission":"!ESC_MISSION!","sender":"!ESC_SENDER!","content":"!ESC_CONTENT!"}"

REM ── 解析 Redis 地址 ──
for /f "tokens=1,2 delims=:" %%a in ("!REDIS_ADDR!") do (
    set "REDIS_HOST=%%a"
    set "REDIS_PORT=%%b"
)
if "!REDIS_PORT!"=="" set "REDIS_PORT=6379"

REM ── 发送消息 ──
echo 正在发送报警...
echo   Channel: !CHANNEL!
echo   Level:   !FINAL_LEVEL!
echo   Title:   !TITLE!
echo.

REM ── 尝试 redis-cli ──
where redis-cli >nul 2>&1
if !errorlevel! equ 0 (
    REM 有 redis-cli，直接用
    set "REDIS_CMD=redis-cli -h !REDIS_HOST! -p !REDIS_PORT! -n !REDIS_DB!"
    if not "!REDIS_PASSWORD!"=="" (
        set "REDIS_CMD=!REDIS_CMD! -a !REDIS_PASSWORD!"
    )
    !REDIS_CMD! PUBLISH !CHANNEL! !JSON_PAYLOAD!
    if !errorlevel! equ 0 (
        echo ✓ 发送成功!
    ) else (
        echo [错误] 发送失败，请检查 Redis 连接
        exit /b 1
    )
) else (
    REM 无 redis-cli，使用 PowerShell 发送 RESP 协议
    echo 未找到 redis-cli，尝试使用 PowerShell 直接发送...
    call :publish_via_powershell
    if !errorlevel! neq 0 (
        echo.
        echo [错误] 发送失败!
        echo 请安装 redis-cli 或检查 Redis 连接配置
        echo   Windows redis-cli 下载: https://github.com/tporadowski/redis/releases
        exit /b 1
    )
)

endlocal
exit /b 0

REM ══════════════════════════════════════════════════════════
REM  JSON 转义子程序
REM ══════════════════════════════════════════════════════════
:json_escape
set "str=%~1"
set "out_var=%~2"
REM 转义反斜杠
set "str=!str:\=\\!"
REM 转义双引号
set "str=!str:"=\"!"
REM 转义换行符（批处理中较少见，但仍处理）
set "str=!str:=\\n!"
set "%out_var%=!str!"
goto :eof

REM ══════════════════════════════════════════════════════════
REM  通过 PowerShell 发送 PUBLISH 命令（RESP 协议）
REM ══════════════════════════════════════════════════════════
:publish_via_powershell
set "PS_SCRIPT=$addr='%REDIS_HOST%';$port=%REDIS_PORT%;$db=%REDIS_DB%;$ch='%CHANNEL%';$msg='%JSON_PAYLOAD%';try{$tcp=New-Object System.Net.Sockets.TcpClient($addr,$port);$stream=$tcp.GetStream();$writer=New-Object System.IO.StreamWriter($stream,[System.Text.Encoding]::ASCII);$reader=New-Object System.IO.StreamReader($stream,[System.Text.Encoding]::ASCII);if('%REDIS_PASSWORD%' -ne ''){$authCmd=\"*2`r`n$$4`r`nauth`r`n$$('%REDIS_PASSWORD%'.Length)`r`n%REDIS_PASSWORD%`r`n\";$writer.Write($authCmd);$writer.Flush();$null=$reader.ReadLine()};if($db -ne 0){$selectCmd=\"*2`r`n$$6`r`nselect`r`n$$($db.ToString().Length)`r`n$db`r`n\";$writer.Write($selectCmd);$writer.Flush();$null=$reader.ReadLine()};$msgLen=$msg.Length;$pubCmd=\"*3`r`n$$9`r`npublish`r`n$$($ch.Length)`r`n$ch`r`n$$msgLen`r`n$msg`r`n\";$writer.Write($pubCmd);$writer.Flush();$resp=$reader.ReadLine();Write-Host $resp;$stream.Close();$tcp.Close();exit 0}catch{Write-Host $_.Exception.Message;exit 1}"

powershell -NoProfile -Command "%PS_SCRIPT%"
exit /b !errorlevel!

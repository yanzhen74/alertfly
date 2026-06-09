//go:build linux

package tray

import (
	"log"
	"os/exec"
	"strings"
)

// TrayApp Linux 上不实现系统托盘，提供空实现保持接口一致
type TrayApp struct {
	webURL string
	onQuit func()
}

// NewTrayApp 创建托盘应用实例（Linux 上为空实现）
func NewTrayApp(webURL string, onQuit func()) *TrayApp {
	return &TrayApp{
		webURL: webURL,
		onQuit: onQuit,
	}
}

// Start Linux 上不做托盘，直接返回（非阻塞）
func (t *TrayApp) Start() {}

// ShowNotification Linux 上通过 notify-send 发送桌面通知
func (t *TrayApp) ShowNotification(title, message string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		log.Printf("[tray] notify-send 不可用，跳过桌面通知: %s - %s", title, message)
		return
	}

	title = truncate(title, 64)
	message = truncate(message, 200)

	cmd := exec.Command("notify-send", title, message)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[tray] notify-send 失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
	}
}

// truncate 截断字符串，最多保留 maxRunes 个 rune
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

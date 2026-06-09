//go:build windows && !win7
// +build windows,!win7

package notifier

import (
	"fmt"
	"log"

	"github.com/oliverxu/alertfly/internal/model"
)

// windowsNotifier Windows 10/11 默认通知器。
// Balloon Tip 通知（Shell_NotifyIconW + NIF_INFO）在 Win10/11 上由系统自动转换为 Toast。
// 实际的通知发送由 trayNotify 回调负责（通过 systray.ShowNotification），
// 此处仅做日志记录，避免了 PowerShell 的 3-6 秒延迟。
type windowsNotifier struct{}

// NewNotifier 根据当前平台创建对应的通知器实例
func NewNotifier() Notifier {
	return &windowsNotifier{}
}

// Notify 记录告警日志（气泡通知由 trayNotify 回调处理）
func (n *windowsNotifier) Notify(msg *model.Message) error {
	title := msg.Title
	if title == "" {
		title = fmt.Sprintf("[%s] %s", msg.Level, msg.Source)
	}
	log.Printf("[notifier] [%s] %s: %s", msg.Level, title, truncate(msg.Content, 200))
	return nil
}

// NotifyError 记录错误日志（气泡通知由 trayNotify 回调处理）
func (n *windowsNotifier) NotifyError(title string, body string) error {
	log.Printf("[notifier] [error] %s: %s", title, body)
	return nil
}

// truncate 截断字符串，最多保留 maxRunes 个 rune
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

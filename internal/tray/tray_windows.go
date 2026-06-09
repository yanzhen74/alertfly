//go:build windows

package tray

import (
	"log"
	"os/exec"

	"github.com/getlantern/systray"
)

// TrayApp 系统托盘应用
type TrayApp struct {
	webURL string
	onQuit func()
}

// NewTrayApp 创建托盘应用实例
func NewTrayApp(webURL string, onQuit func()) *TrayApp {
	return &TrayApp{
		webURL: webURL,
		onQuit: onQuit,
	}
}

// Start 启动系统托盘（阻塞），systray.Run 在 Windows 上要求在主线程调用
func (t *TrayApp) Start() {
	systray.Run(t.onReady, t.onExit)
}

// ShowNotification 通过 PowerShell Toast 发送 Windows 通知（可从任意 goroutine 调用）
func (t *TrayApp) ShowNotification(title, message string) {
	title = truncate(title, 64)
	message = truncate(message, 200)

	script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$toastXml = [xml] $template.GetXml()
$toastXml.GetElementsByTagName("text")[0].AppendChild($toastXml.CreateTextNode("` + title + `")) | Out-Null
$toastXml.GetElementsByTagName("text")[1].AppendChild($toastXml.CreateTextNode("` + message + `")) | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($toastXml.OuterXml)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("AlertFly").Show($toast)`

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err := cmd.Start(); err != nil {
		log.Printf("[tray] ShowNotification 启动失败: %v", err)
	}
}

// onReady 在 systray 初始化完成后回调，设置图标和菜单
func (t *TrayApp) onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("AlertFly")
	systray.SetTooltip("AlertFly - 告警通知")

	mOpenUI := systray.AddMenuItem("打开 Web UI", "在浏览器中打开 AlertFly 面板")
	mSettings := systray.AddMenuItem("设置", "打开 AlertFly 设置页面")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出 AlertFly")

	go func() {
		for {
			select {
			case <-mOpenUI.ClickedCh:
				openURL(t.webURL)
			case <-mSettings.ClickedCh:
				openURL(t.webURL + "/settings.html")
			case <-mQuit.ClickedCh:
				log.Println("[tray] 用户通过托盘菜单选择退出")
				systray.Quit()
			}
		}
	}()
}

// onExit 在 systray.Quit() 被调用后回调
func (t *TrayApp) onExit() {
	if t.onQuit != nil {
		t.onQuit()
	}
}

// openURL 使用 Windows 默认浏览器打开指定 URL
func openURL(url string) {
	cmd := exec.Command("cmd", "/c", "start", url)
	if err := cmd.Start(); err != nil {
		log.Printf("[tray] 打开 URL 失败 %s: %v", url, err)
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

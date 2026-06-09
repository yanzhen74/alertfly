//go:build windows

package tray

import (
	"log"
	"os/exec"
	"unsafe"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
)

var (
	shell32           = windows.NewLazySystemDLL("Shell32.dll")
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	user32       = windows.NewLazySystemDLL("User32.dll")
	pFindWindowW = user32.NewProc("FindWindowW")
)

// notifyIconData 用于 Shell_NotifyIconW 的气泡通知结构体
// https://learn.microsoft.com/en-us/windows/win32/api/shellapi/ns-shellapi-notifyicondataw
type notifyIconData struct {
	Size                       uint32
	Wnd                        windows.Handle
	ID                         uint32
	Flags                      uint32
	CallbackMessage            uint32
	Icon                       windows.Handle
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout, Version           uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   windows.GUID
	BalloonIcon                windows.Handle
}

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

// ShowNotification 通过 Balloon Tip 发送 Windows 气泡通知（Win7+ 兼容，可从任意 goroutine 调用）
func (t *TrayApp) ShowNotification(title, message string) {
	title = truncate(title, 48)   // szInfoTitle 最多 48 字符（不含 null）
	message = truncate(message, 200)

	const (
		NIM_MODIFY = 0x00000001
		NIF_INFO   = 0x00000010
		NIIF_INFO  = 0x00000001 // 信息图标
	)

	// 查找 systray 创建的隐藏窗口，窗口类名为 "SystrayClass"
	classNamePtr, err := windows.UTF16PtrFromString("SystrayClass")
	if err != nil {
		log.Printf("[tray] ShowNotification UTF16 转换失败: %v", err)
		return
	}
	hwnd, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(classNamePtr)), 0)
	if hwnd == 0 {
		log.Printf("[tray] ShowNotification 找不到 systray 窗口，跳过气泡通知")
		return
	}

	titleUTF16, err := windows.UTF16FromString(title)
	if err != nil {
		log.Printf("[tray] ShowNotification 标题转换失败: %v", err)
		return
	}
	infoUTF16, err := windows.UTF16FromString(message)
	if err != nil {
		log.Printf("[tray] ShowNotification 消息转换失败: %v", err)
		return
	}

	nid := notifyIconData{
		Wnd:       windows.Handle(hwnd),
		ID:        100, // systray 内部使用的 NotifyIcon ID
		Flags:     NIF_INFO,
		InfoFlags: NIIF_INFO,
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	copy(nid.InfoTitle[:], titleUTF16)
	copy(nid.Info[:], infoUTF16)

	res, _, err := pShellNotifyIconW.Call(
		uintptr(NIM_MODIFY),
		uintptr(unsafe.Pointer(&nid)),
	)
	if res == 0 {
		log.Printf("[tray] ShowNotification Shell_NotifyIconW 失败: %v", err)
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

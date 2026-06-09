package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// VersionInfo 远程版本信息
type VersionInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// Config 更新器配置
type Config struct {
	Enabled  bool
	CheckURL string        // 版本检查 URL，返回 VersionInfo JSON
	Interval time.Duration // 轮询间隔
}

// ErrorNotifier 错误通知回调（用于弹窗警告）
type ErrorNotifier func(title string, body string)

// Updater 自更新管理器
type Updater struct {
	cfg            Config
	currentVersion string
	notifyError    ErrorNotifier
	client         *http.Client
}

// NewUpdater 创建更新器
func NewUpdater(cfg Config, currentVersion string, notifyError ErrorNotifier) *Updater {
	return &Updater{
		cfg:            cfg,
		currentVersion: currentVersion,
		notifyError:    notifyError,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Start 启动定时检查（非阻塞，内部启动 goroutine）
func (u *Updater) Start(ctx context.Context) {
	if !u.cfg.Enabled {
		log.Printf("[updater] 自动更新已禁用，跳过启动")
		return
	}

	log.Printf("[updater] 启动定时检查，间隔 %v，检查地址 %s", u.cfg.Interval, u.cfg.CheckURL)

	go func() {
		// 启动时立即检查一次
		if err := u.CheckAndUpdate(); err != nil {
			log.Printf("[updater] 启动时检查更新失败: %v", err)
		}

		ticker := time.NewTicker(u.cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Printf("[updater] 收到取消信号，停止定时检查")
				return
			case <-ticker.C:
				if err := u.CheckAndUpdate(); err != nil {
					log.Printf("[updater] 定时检查更新失败: %v", err)
				}
			}
		}
	}()
}

// CheckAndUpdate 立即检查并执行更新
func (u *Updater) CheckAndUpdate() error {
	log.Printf("[updater] 开始检查更新...")

	// 1. 获取远程版本信息
	info, err := u.fetchVersionInfo()
	if err != nil {
		u.notifyError("更新检查失败", fmt.Sprintf("无法获取远程版本信息: %v", err))
		return fmt.Errorf("fetch version info: %w", err)
	}

	log.Printf("[updater] 远程版本: %s, 当前版本: %s", info.Version, u.currentVersion)

	// 2. 比较版本
	if !needsUpdate(u.currentVersion, info.Version) {
		log.Printf("[updater] 当前版本已是最新，无需更新")
		return nil
	}

	log.Printf("[updater] 发现新版本 %s，开始更新...", info.Version)

	// 3. 下载新版本到临时文件
	tmpPath, err := u.download(info)
	if err != nil {
		u.notifyError("更新下载失败", fmt.Sprintf("下载新版本 %s 失败: %v", info.Version, err))
		return fmt.Errorf("download: %w", err)
	}

	// 4. SHA256 校验
	if err := u.verifySHA256(tmpPath, info.SHA256); err != nil {
		u.notifyError("更新校验失败", fmt.Sprintf("SHA256 校验失败: %v", err))
		u.cleanupTmp(tmpPath)
		return fmt.Errorf("verify sha256: %w", err)
	}

	// 5. 替换当前可执行文件
	if err := u.replace(tmpPath); err != nil {
		u.notifyError("更新替换失败", fmt.Sprintf("替换可执行文件失败: %v", err))
		u.cleanupTmp(tmpPath)
		return fmt.Errorf("replace: %w", err)
	}

	log.Printf("[updater] 更新完成，即将重启至版本 %s", info.Version)

	// 6. 重启
	u.restart()

	return nil
}

// fetchVersionInfo 从远程 URL 获取版本信息
func (u *Updater) fetchVersionInfo() (*VersionInfo, error) {
	resp, err := u.client.Get(u.cfg.CheckURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", u.cfg.CheckURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体: %w", err)
	}

	var info VersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("解析 JSON: %w", err)
	}

	if info.Version == "" || info.URL == "" {
		return nil, fmt.Errorf("版本信息不完整: version 或 url 为空")
	}

	return &info, nil
}

// download 下载新版本到临时文件（同目录下 .alertfly.update.tmp）
func (u *Updater) download(info *VersionInfo) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取当前可执行文件路径: %w", err)
	}

	// resolve symlink if needed
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("解析符号链接: %w", err)
	}

	tmpPath := filepath.Join(filepath.Dir(exePath), ".alertfly.update.tmp")

	resp, err := u.client.Get(info.URL)
	if err != nil {
		return "", fmt.Errorf("HTTP GET %s: %w", info.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 HTTP 状态码 %d", resp.StatusCode)
	}

	// 先写入 .download 中间文件，下载完成后再重命名为 .tmp
	// 这样可以避免下载中断时残留不完整的 .tmp 文件
	downloadPath := tmpPath + ".download"
	f, err := os.OpenFile(downloadPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("创建临时文件: %w", err)
	}

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(downloadPath)
		return "", fmt.Errorf("写入下载内容: %w", err)
	}
	f.Close()

	log.Printf("[updater] 下载完成，大小 %d bytes", written)

	// 重命名为最终临时文件名
	if err := os.Rename(downloadPath, tmpPath); err != nil {
		os.Remove(downloadPath)
		return "", fmt.Errorf("重命名下载文件: %w", err)
	}

	return tmpPath, nil
}

// verifySHA256 校验下载文件的 SHA256
func (u *Updater) verifySHA256(tmpPath, expected string) error {
	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("打开临时文件: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算 SHA256: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("SHA256 不匹配: 期望 %s, 实际 %s", expected, actual)
	}

	log.Printf("[updater] SHA256 校验通过")
	return nil
}

// replace 替换当前可执行文件
func (u *Updater) replace(tmpPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("解析符号链接: %w", err)
	}

	oldPath := exePath + ".old"

	// 删除可能残留的 .old 文件
	os.Remove(oldPath)

	// 重命名当前可执行文件为 .old
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("重命名当前文件为 .old: %w", err)
	}

	// 重命名临时文件为当前可执行文件路径
	if err := os.Rename(tmpPath, exePath); err != nil {
		// 回滚：尝试将 .old 恢复为原始文件
		if renameErr := os.Rename(oldPath, exePath); renameErr != nil {
			log.Printf("[updater] 回滚恢复失败: %v (原始文件可能丢失)", renameErr)
		}
		return fmt.Errorf("重命名临时文件为当前路径: %w", err)
	}

	// 设置可执行权限
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if err := os.Chmod(exePath, 0755); err != nil {
			log.Printf("[updater] 设置可执行权限失败: %v (可能仍可执行)", err)
		}
	}

	// 清理 .old 文件（延迟删除，因为当前进程还在使用）
	// 在重启后旧进程不再占用，可安全删除
	go func() {
		time.Sleep(5 * time.Second)
		if err := os.Remove(oldPath); err != nil {
			log.Printf("[updater] 清理 .old 文件失败: %v", err)
		}
	}()

	log.Printf("[updater] 文件替换完成")
	return nil
}

// restart 重启程序
func (u *Updater) restart() {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("[updater] 获取可执行文件路径失败，无法重启: %v", err)
		u.notifyError("更新重启失败", fmt.Sprintf("获取可执行文件路径失败: %v", err))
		return
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		log.Printf("[updater] 解析符号链接失败，无法重启: %v", err)
		u.notifyError("更新重启失败", fmt.Sprintf("解析符号链接失败: %v", err))
		return
	}

	if runtime.GOOS == "windows" {
		// Windows: 启动新进程，退出当前进程
		attr := &os.ProcAttr{
			Dir:   filepath.Dir(exePath),
			Env:   os.Environ(),
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		}
		process, err := os.StartProcess(exePath, os.Args, attr)
		if err != nil {
			log.Printf("[updater] Windows 启动新进程失败: %v", err)
			u.notifyError("更新重启失败", fmt.Sprintf("启动新进程失败: %v", err))
			return
		}
		process.Release()
		os.Exit(0)
	}

	// Linux/Darwin: 使用 syscall.Exec 原地重启（替换当前进程）
	log.Printf("[updater] 使用 syscall.Exec 原地重启: %s", exePath)
	err = syscall.Exec(exePath, os.Args, os.Environ())
	if err != nil {
		log.Printf("[updater] syscall.Exec 失败: %v", err)
		u.notifyError("更新重启失败", fmt.Sprintf("syscall.Exec 失败: %v", err))
		// syscall.Exec 失败时进程仍在运行，尝试用 StartProcess 作为备选
		attr := &os.ProcAttr{
			Dir:   filepath.Dir(exePath),
			Env:   os.Environ(),
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		}
		process, startErr := os.StartProcess(exePath, os.Args, attr)
		if startErr != nil {
			log.Printf("[updater] 备选重启方式也失败: %v", startErr)
			return
		}
		process.Release()
		os.Exit(0)
	}
}

// cleanupTmp 清理临时文件
func (u *Updater) cleanupTmp(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[updater] 清理临时文件失败: %v", err)
	}
}

// needsUpdate 判断是否需要更新（语义化版本比较）
// 返回 true 表示远程版本 > 当前版本
func needsUpdate(current, remote string) bool {
	cv := parseSemVer(current)
	rv := parseSemVer(remote)

	for i := 0; i < 3; i++ {
		if rv[i] > cv[i] {
			return true
		}
		if rv[i] < cv[i] {
			return false
		}
	}
	// 版本完全相等，不需要更新
	return false
}

// parseSemVer 解析语义化版本字符串，返回 [major, minor, patch]
// 支持 "v1.2.3" 和 "1.2.3" 格式
// 解析失败时返回 [0, 0, 0]
func parseSemVer(v string) [3]int {
	result := [3]int{0, 0, 0}

	// 去除 "v" 或 "V" 前缀
	v = strings.TrimLeft(v, "vV")

	parts := strings.SplitN(v, ".", 3)
	for i, p := range parts {
		// 去除可能的后缀（如 "-beta", "-rc1" 等）
		p = strings.SplitN(p, "-", 2)[0]
		n, err := strconv.Atoi(p)
		if err != nil {
			// 解析失败，保留 0
			continue
		}
		result[i] = n
	}

	return result
}
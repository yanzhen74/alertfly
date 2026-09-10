// Package logger 提供带级别控制和文件轮转的轻量日志器。
//
// 设计目标：
//   - 兼容 Go 1.17+
//   - 支持 DEBUG/INFO/WARN/ERROR 四个级别
//   - 同时输出到 stderr 和可选的日志文件
//   - 文件大小超过阈值时自动轮转（保留 1 个备份）
//   - 运行时可动态调整日志级别
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

// String 返回级别的字符串表示
func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel 解析级别字符串（大小写不敏感）
func ParseLevel(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN", "WARNING":
		return WARN
	case "ERROR", "ERR":
		return ERROR
	default:
		return INFO
	}
}

// Config 日志配置
type Config struct {
	Level      string `yaml:"level" json:"level"`           // 日志级别：debug/info/warn/error
	FilePath   string `yaml:"file_path" json:"file_path"`   // 日志文件路径，空=仅输出到 stderr
	MaxSizeMB  int    `yaml:"max_size_mb" json:"max_size_mb"` // 单个日志文件最大 MB，默认 10
}

// Logger 日志器
type Logger struct {
	mu        sync.Mutex
	level     Level
	stderr    *log.Logger
	file      *os.File
	fileLog   *log.Logger
	filePath  string
	maxSize   int64 // 字节
}

var (
	defaultLogger *Logger
	once          sync.Once
)

func init() {
	// 默认日志器：仅输出到 stderr，INFO 级别
	defaultLogger = &Logger{
		level:  INFO,
		stderr: log.New(os.Stderr, "", log.LstdFlags),
	}
}

// Init 初始化全局日志器。应在 main 函数中调用一次。
func Init(cfg Config) error {
	level := ParseLevel(cfg.Level)

	maxSize := int64(10 * 1024 * 1024) // 默认 10MB
	if cfg.MaxSizeMB > 0 {
		maxSize = int64(cfg.MaxSizeMB) * 1024 * 1024
	}

	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	defaultLogger.level = level
	defaultLogger.maxSize = maxSize

	// 关闭旧文件
	if defaultLogger.file != nil {
		defaultLogger.file.Close()
		defaultLogger.file = nil
		defaultLogger.fileLog = nil
	}

	// 打开日志文件
	if cfg.FilePath != "" {
		dir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建日志目录失败: %w", err)
		}

		f, err := os.OpenFile(cfg.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("打开日志文件失败: %w", err)
		}

		defaultLogger.file = f
		defaultLogger.filePath = cfg.FilePath
		defaultLogger.fileLog = log.New(f, "", log.LstdFlags)

		// 启动时检查是否需要轮转
		defaultLogger.checkRotate()
	}

	return nil
}

// SetLevel 运行时动态调整日志级别
func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// GetLevel 获取当前日志级别
func GetLevel() Level {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	return defaultLogger.level
}

// Debug 输出 DEBUG 级别日志
func Debug(format string, args ...interface{}) {
	defaultLogger.log(DEBUG, format, args...)
}

// Info 输出 INFO 级别日志
func Info(format string, args ...interface{}) {
	defaultLogger.log(INFO, format, args...)
}

// Warn 输出 WARN 级别日志
func Warn(format string, args ...interface{}) {
	defaultLogger.log(WARN, format, args...)
}

// Error 输出 ERROR 级别日志
func Error(format string, args ...interface{}) {
	defaultLogger.log(ERROR, format, args...)
}

// Debugf 同 Debug，兼容 fmt.Printf 风格
func Debugf(format string, args ...interface{}) {
	defaultLogger.log(DEBUG, format, args...)
}

// Infof 同 Info
func Infof(format string, args ...interface{}) {
	defaultLogger.log(INFO, format, args...)
}

// Warnf 同 Warn
func Warnf(format string, args ...interface{}) {
	defaultLogger.log(WARN, format, args...)
}

// Errorf 同 Error
func Errorf(format string, args ...interface{}) {
	defaultLogger.log(ERROR, format, args...)
}

// Println 兼容 log.Println，使用 INFO 级别
func Println(args ...interface{}) {
	defaultLogger.log(INFO, "%s", fmt.Sprintln(args...))
}

// Printf 兼容 log.Printf，使用 INFO 级别
func Printf(format string, args ...interface{}) {
	defaultLogger.log(INFO, format, args...)
}

// log 内部日志输出方法
func (l *Logger) log(level Level, format string, args ...interface{}) {
	l.mu.Lock()
	currentLevel := l.level
	l.mu.Unlock()

	if level < currentLevel {
		return
	}

	msg := fmt.Sprintf(format, args...)
	prefix := "[" + level.String() + "] "
	fullMsg := prefix + msg

	// 输出到 stderr
	l.stderr.Output(2, fullMsg)

	// 输出到文件（如果有）
	l.mu.Lock()
	if l.fileLog != nil {
		l.fileLog.Output(2, fullMsg)
		l.checkRotate()
	}
	l.mu.Unlock()
}

// checkRotate 检查是否需要轮转日志文件。调用者需持有锁。
func (l *Logger) checkRotate() {
	if l.file == nil || l.filePath == "" {
		return
	}

	info, err := l.file.Stat()
	if err != nil {
		return
	}

	if info.Size() < l.maxSize {
		return
	}

	// 关闭当前文件
	l.file.Close()

	// 删除旧的备份文件
	backupPath := l.filePath + ".1"
	os.Remove(backupPath)

	// 当前文件重命名为备份
	os.Rename(l.filePath, backupPath)

	// 创建新文件
	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		// 回退到仅 stderr 输出
		l.file = nil
		l.fileLog = nil
		l.stderr.Output(2, "[ERROR] 日志轮转后重新打开文件失败: "+err.Error())
		return
	}

	l.file = f
	l.fileLog = log.New(f, "", log.LstdFlags)
	l.stderr.Output(2, "[INFO] 日志文件已轮转")
}

// Close 关闭日志器（释放文件句柄）
func Close() {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	if defaultLogger.file != nil {
		defaultLogger.file.Close()
		defaultLogger.file = nil
		defaultLogger.fileLog = nil
	}
}

// Writer 返回一个 io.Writer，兼容需要 io.Writer 的场景。
// 写入的内容按 INFO 级别输出。
func Writer() io.Writer {
	return &logWriter{}
}

type logWriter struct{}

func (w *logWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimRight(string(p), "\n")
	defaultLogger.log(INFO, "%s", msg)
	return len(p), nil
}

// FormatTimestamp 返回当前时间戳字符串，用于自定义格式
func FormatTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

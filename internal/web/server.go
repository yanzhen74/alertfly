package web

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/storage"
)

// WebServer HTTP Web 服务器，提供前端页面和 REST API
type WebServer struct {
	port       int
	configPath string
	storage    storage.Storage
	config     *config.Config
	engine     *gin.Engine
	server     *http.Server
	onCheckUpdate func() error // 立即检查更新回调
}

// NewWebServer 创建 Web 服务器实例
func NewWebServer(port int, configPath string, store storage.Storage, cfg *config.Config) *WebServer {
	// 设置 Gin 为 Release 模式，减少日志输出
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())

	s := &WebServer{
		port:      port,
		configPath: configPath,
		storage:   store,
		config:    cfg,
		engine:    engine,
	}

	s.registerRoutes()
	return s
}

// registerRoutes 注册所有路由
func (s *WebServer) registerRoutes() {
	// 静态资源（通过 embed.FS 提供）
	// 为 css 和 js 分别创建子目录 FS，确保路径映射正确
	cssFS, err := fs.Sub(StaticFS, "static/css")
	if err != nil {
		log.Printf("[web] 创建 CSS 子目录 FS 失败: %v", err)
	} else {
		s.engine.StaticFS("/css", http.FS(cssFS))
	}

	jsFS, err := fs.Sub(StaticFS, "static/js")
	if err != nil {
		log.Printf("[web] 创建 JS 子目录 FS 失败: %v", err)
	} else {
		s.engine.StaticFS("/js", http.FS(jsFS))
	}

	fontFS, err := fs.Sub(StaticFS, "static/font")
	if err != nil {
		log.Printf("[web] 创建 Font 子目录 FS 失败: %v", err)
	} else {
		s.engine.StaticFS("/font", http.FS(fontFS))
	}

	// HTML 页面路由 — 直接读取 embed 文件内容，避免 http.FileServer 重定向循环
	s.engine.GET("/", func(c *gin.Context) {
		data, err := StaticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "读取页面失败")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
	s.engine.GET("/index.html", func(c *gin.Context) {
		data, err := StaticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "读取页面失败")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
	s.engine.GET("/settings.html", func(c *gin.Context) {
		data, err := StaticFS.ReadFile("static/settings.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "读取页面失败")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// API 路由
	s.engine.GET("/api/messages", s.handleMessages)
	s.engine.GET("/api/config", s.handleGetConfig)
	s.engine.PUT("/api/config", s.handleUpdateConfig)
	s.engine.GET("/api/status", s.handleStatus)
	s.engine.POST("/api/update/check", s.handleCheckUpdate)
}

// Start 启动 HTTP 服务（非阻塞，内部启动 goroutine）
func (s *WebServer) Start() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}

	go func() {
		log.Printf("[web] ════════════════════════════════════════")
		log.Printf("[web] AlertFly Web UI 已启动")
		log.Printf("[web] 访问地址: http://127.0.0.1:%d", s.port)
		log.Printf("[web] ════════════════════════════════════════")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[web] HTTP 服务器异常退出: %v", err)
		}
	}()

	// 启动后自动打开浏览器
	go s.openBrowser()

	return nil
}

// SetCheckUpdateHandler 设置立即检查更新的回调函数
func (s *WebServer) SetCheckUpdateHandler(fn func() error) {
	s.onCheckUpdate = fn
}

// handleCheckUpdate POST /api/update/check — 立即检查更新
func (s *WebServer) handleCheckUpdate(c *gin.Context) {
	if s.onCheckUpdate == nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "更新功能未初始化"})
		return
	}
	if err := s.onCheckUpdate(); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已是最新版本，无需更新"})
}

// Stop 优雅关闭 HTTP 服务器
func (s *WebServer) Stop() error {
	if s.server == nil {
		return nil
	}

	log.Println("[web] 正在关闭 HTTP 服务器...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("关闭 HTTP 服务器失败: %w", err)
	}
	log.Println("[web] HTTP 服务器已关闭")
	return nil
}

// openBrowser 自动打开系统浏览器访问 Web 界面
func (s *WebServer) openBrowser() {
	url := fmt.Sprintf("http://127.0.0.1:%d", s.port)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		log.Printf("[web] 不支持的平台 %s，无法自动打开浏览器", runtime.GOOS)
		return
	}

	if err := cmd.Run(); err != nil {
		log.Printf("[web] 自动打开浏览器失败: %v", err)
	}
}

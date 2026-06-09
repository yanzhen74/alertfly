package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/consumer"
	"github.com/oliverxu/alertfly/internal/model"
	"github.com/oliverxu/alertfly/internal/notifier"
	"github.com/oliverxu/alertfly/internal/proxy"
	"github.com/oliverxu/alertfly/internal/storage"
	"github.com/oliverxu/alertfly/internal/tray"
	"github.com/oliverxu/alertfly/internal/updater"
	"github.com/oliverxu/alertfly/internal/web"
)

// version 由 build.sh 通过 ldflags 注入
var version = "dev"

func main() {
	// --- 命令行参数解析 ---
	configPath := flag.String("config", "./config.yaml", "配置文件路径")
	redisAddr := flag.String("redis-addr", "", "Redis 地址（覆盖配置文件）")
	kafkaBrokers := flag.String("kafka-brokers", "", "Kafka brokers，逗号分隔（覆盖配置文件）")
	stdout := flag.Bool("stdout", false, "启用标准输出模式")
	noTray := flag.Bool("no-tray", false, "禁用系统托盘（Windows 专用，以服务模式运行时使用）")
	flag.Parse()

	// --- 加载配置 ---
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("[main] 加载配置文件失败: %v", err)
	}

	// 命令行参数覆盖配置文件
	if *redisAddr != "" {
		cfg.Redis.Addr = *redisAddr
	}
	if *kafkaBrokers != "" {
		cfg.Kafka.Brokers = strings.Split(*kafkaBrokers, ",")
	}

	// 默认值补全
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.Redis.Mode == "" {
		cfg.Redis.Mode = "pubsub"
	}
	if cfg.Redis.Channel == "" {
		cfg.Redis.Channel = "alerts"
	}
	if cfg.Kafka.Topic == "" {
		cfg.Kafka.Topic = "alerts"
	}
	if cfg.Kafka.GroupID == "" {
		cfg.Kafka.GroupID = "alertfly_group"
	}
	if cfg.Storage.DBPath == "" {
		cfg.Storage.DBPath = "./alertfly.db"
	}
	if cfg.Storage.RetentionDays == 0 {
		cfg.Storage.RetentionDays = 7
	}
	if cfg.Storage.MaxRecords == 0 {
		cfg.Storage.MaxRecords = 10000
	}
	// 默认启用至少一个消费者（兼容旧配置文件无 enabled 字段的情况）
	if !cfg.Redis.Enabled && !cfg.Kafka.Enabled {
		cfg.Redis.Enabled = true
	}

	// Web 配置默认值
	if cfg.Web.Port == 0 {
		cfg.Web.Port = 18080
	}

	// --- 优雅退出 ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("[main] 收到信号 %v，开始优雅退出...", sig)
		cancel()
	}()

	// --- 初始化系统托盘 ---
	// Windows 上：tray.Start() 是阻塞的（systray.Run），必须在主线程调用。
	//            业务主循环在下方 goroutine 中运行，main 最后调用 tray.Start()。
	// Linux 上：tray.Start() 是 noop，main 最后的调用立即返回，再等待 doneCh。
	webURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.Web.Port)
	trayApp := tray.NewTrayApp(webURL, func() {
		log.Println("[main] 托盘退出回调，触发程序退出")
		cancel()
	})

	// doneCh 用于等待业务 goroutine 完成
	doneCh := make(chan struct{})

	// 将业务逻辑放入 goroutine，使主线程可以运行托盘（Windows 需要主线程运行消息泵）
	go func() {
		defer close(doneCh)
		runApp(ctx, cancel, cfg, configPath, stdout, trayApp)
	}()

	// --- 启动系统托盘（Windows 阻塞；Linux noop 立即返回）---
	// Windows：此处阻塞，直到用户从托盘选择"退出"或调用 systray.Quit()
	// Linux：立即返回，继续等待 doneCh
	if !*noTray {
		trayApp.Start()
	}

	// 等待业务 goroutine 退出（Linux 上主要靠此处阻塞）
	<-doneCh
	log.Println("[main] AlertFly 已退出")
}

// runApp 包含 AlertFly 的全部业务逻辑，在独立 goroutine 中运行。
// 将业务逻辑从 main 中分离出来，使主线程可以运行 systray（Windows 需要）。
func runApp(ctx context.Context, cancel context.CancelFunc,
	cfg *config.Config, configPath *string, stdout *bool,
	trayApp *tray.TrayApp) {

	// --- 初始化 Storage ---
	store, err := storage.NewSQLiteStorage(cfg.Storage.DBPath)
	if err != nil {
		log.Fatalf("[main] 初始化 SQLite 存储失败: %v", err)
	}
	defer store.Close()
	log.Println("[main] SQLite 存储初始化成功")

	// --- 创建更新事件记录回调 ---
	recordUpdateEvent := func(event updater.Event) {
		msg := &model.Message{
			Source:     "system",
			Topic:      "update",
			Level:      "info",
			SubType:    "update",
			Sender:     "system",
			ReceivedAt: time.Now(),
		}

		switch event.Kind {
		case updater.EventFoundNewVersion:
			msg.Level = "info"
			msg.Title = fmt.Sprintf("发现新版本 %s", event.Version)
			msg.Content = fmt.Sprintf("新版本: %s, 下载地址: %s", event.Version, event.URL)
		case updater.EventAlreadyLatest:
			// "已是最新版本" 不记录到数据库，避免定时检查产生大量无意义记录
			log.Printf("[updater] 版本检查完成，已是最新版本")
			return
		case updater.EventCheckFailed:
			msg.Level = "warn"
			msg.Title = "版本检查失败"
			msg.Content = event.Err.Error()
		case updater.EventUpdateSuccess:
			msg.Level = "info"
			msg.Title = "版本更新成功，将在重启后生效"
			msg.Content = fmt.Sprintf("新版本: %s", event.Version)
		case updater.EventUpdateFailed:
			msg.Level = "error"
			msg.Title = "版本更新失败"
			msg.Content = event.Err.Error()
		}

		if saveErr := store.Save(msg); saveErr != nil {
			log.Printf("[main] 保存更新事件失败: %v", saveErr)
		}
	}

	// --- 初始化 Web 服务器 ---
	ws := web.NewWebServer(cfg.Web.Port, *configPath, store, cfg)
	if err := ws.Start(); err != nil {
		log.Printf("[main] Web UI 启动失败: %v", err)
	}

	// --- 初始化 Updater（自更新）---
	var ud *updater.Updater
	if cfg.Updater.CheckURL != "" {
		udCfg := updater.Config{
			Enabled:  cfg.Updater.Enabled,
			CheckURL: cfg.Updater.CheckURL,
			Interval: cfg.Updater.Interval,
		}
		if udCfg.Interval == 0 {
			udCfg.Interval = 24 * time.Hour
		}
		ud = updater.NewUpdater(udCfg, version, func(title string, body string) {
			log.Printf("[updater] %s: %s", title, body)
		}, recordUpdateEvent)
		ud.Start(ctx)
		log.Println("[main] Updater 已启动")
	}
	// 注册立即检查更新回调
	ws.SetCheckUpdateHandler(func() *web.UpdateCheckResult {
		// 如果 updater 未初始化（启动时无 check_url），尝试用当前配置动态创建
		if ud == nil {
			if cfg.Updater.CheckURL == "" {
				return &web.UpdateCheckResult{ErrMsg: "自更新未配置，请先设置检查地址并重启程序"}
			}
			udCfg := updater.Config{
				Enabled:  true,
				CheckURL: cfg.Updater.CheckURL,
				Interval: cfg.Updater.Interval,
			}
			if udCfg.Interval == 0 {
				udCfg.Interval = 24 * time.Hour
			}
			ud = updater.NewUpdater(udCfg, version, func(title string, body string) {
				log.Printf("[updater] %s: %s", title, body)
			}, recordUpdateEvent)
			ud.Start(ctx)
			log.Println("[main] Updater 动态初始化完成")
		}
		result := ud.CheckAndUpdate()
		errMsg := ""
		if result.Err != nil {
			errMsg = result.Err.Error()
		}
		return &web.UpdateCheckResult{
			HasUpdate:  result.HasUpdate,
			NewVersion: result.NewVersion,
			Updated:    result.Updated,
			ErrMsg:     errMsg,
		}
	})

	// --- 初始化 Proxy ---
	px := proxy.NewProxy()
	px.RegisterAdapter(&proxy.DefaultJSONAdapter{})
	px.SetDefault("json")
	log.Println("[main] Proxy 初始化完成，注册默认 JSON 适配器")

	// --- 初始化 Notifier ---
	var nt notifier.Notifier
	if cfg.Notifier.Enabled {
		nt = notifier.NewNotifier()
		log.Println("[main] Notifier 初始化成功")
	} else {
		nt = &logNotifier{}
		log.Println("[main] Notifier 已禁用，使用日志替代")
	}

	// --- 初始化异步通知包装器 ---
	asyncNt := notifier.NewAsyncNotifier(nt, trayApp.ShowNotification)
	asyncNt.Start(ctx)
	nt = asyncNt // 后续所有 nt 调用自动走异步限流
	log.Println("[main] AsyncNotifier 已启动（限流: 1s 间隔，合并: >3 条摘要）")

	// --- 初始化并启动 Consumer（带重试） ---
	// 支持同时启用 Redis 和 Kafka 两个消费者
	type consumerEntry struct {
		consumer.Consumer
		name string
	}
	var consumers []consumerEntry

	createAndStart := func(name string, createFn func() (consumer.Consumer, error)) (consumer.Consumer, error) {
		retryInterval := 5 * time.Second
		const maxRetryInterval = 60 * time.Second
		for {
			c, createErr := createFn()
			if createErr != nil {
				c = nil
			} else {
				if startErr := c.Start(ctx); startErr != nil {
					c.Close()
					c = nil
					createErr = fmt.Errorf("启动 %s 消费者失败: %w", name, startErr)
				}
			}
			if createErr == nil {
				log.Printf("[main] %s 消费者已启动", name)
				return c, nil
			}

			log.Printf("[main] %v，%v 后重试...", createErr, retryInterval)
			if notifyErr := nt.NotifyError(name+"消费者启动失败", createErr.Error()); notifyErr != nil {
				log.Printf("[main] 发送错误通知失败: %v", notifyErr)
			}

			select {
			case <-ctx.Done():
				log.Println("[main] 收到退出信号，停止重试")
				return nil, fmt.Errorf("退出")
			case <-time.After(retryInterval):
			}

			retryInterval *= 2
			if retryInterval > maxRetryInterval {
				retryInterval = maxRetryInterval
			}
		}
	}

	if cfg.Redis.Enabled {
		c, err := createAndStart("Redis", func() (consumer.Consumer, error) {
			return consumer.NewRedisConsumer(&cfg.Redis)
		})
		if err == nil && c != nil {
			consumers = append(consumers, consumerEntry{c, "Redis"})
		}
	}

	if cfg.Kafka.Enabled {
		c, err := createAndStart("Kafka", func() (consumer.Consumer, error) {
			return consumer.NewKafkaConsumer(&cfg.Kafka)
		})
		if err == nil && c != nil {
			consumers = append(consumers, consumerEntry{c, "Kafka"})
		}
	}

	if len(consumers) == 0 {
		log.Fatalf("[main] 未启用任何消费者，请启用 Redis 或 Kafka")
	}

	// --- 定期清理 goroutine ---
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := store.Cleanup(cfg.Storage.RetentionDays, cfg.Storage.MaxRecords); err != nil {
					log.Printf("[main] 存储清理失败: %v", err)
				} else {
					log.Println("[main] 存储清理完成")
				}
			}
		}
	}()

	// --- 主循环 ---
	// 合并所有消费者的消息通道和错误通道
	msgCh := make(chan *model.Message)
	errCh := make(chan error)
	for i := range consumers {
		ce := consumers[i]
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case m, ok := <-ce.Messages():
					if !ok {
						return
					}
					msgCh <- m
				case e, ok := <-ce.Errors():
					if !ok {
						return
					}
					errCh <- e
				}
			}
		}()
	}

	log.Println("[main] AlertFly 主循环启动，等待消息...")
	for {
		select {
		case <-ctx.Done():
			log.Println("[main] 主循环退出")
			goto shutdown
		case msg, ok := <-msgCh:
			if !ok {
				log.Println("[main] 消息通道已关闭")
				goto shutdown
			}

			// 通过 Proxy 层二次处理：如果 Consumer 层未能解析（Title == "Raw Message"），
			// 则尝试用 Proxy.Transform 重新解析
			if msg.Title == "Raw Message" && msg.Content != "" {
				transformed, err := px.Transform(msg.Topic, []byte(msg.Content))
				if err == nil && transformed != nil {
					// 保留 Consumer 设置的 Source 和 ReceivedAt
					if transformed.Source == "" {
						transformed.Source = msg.Source
					}
					if transformed.ReceivedAt.IsZero() {
						transformed.ReceivedAt = msg.ReceivedAt
					}
					msg = transformed
				}
				// 如果 Proxy 也解析失败，继续使用原始 msg
			}

			// 存入 Storage
			if err := store.Save(msg); err != nil {
				log.Printf("[main] 存储消息失败: %v", err)
			}

			// 通过异步通知器弹窗通知（含系统托盘通知，限流合并）
			if err := nt.Notify(msg); err != nil {
				log.Printf("[main] 发送通知失败: %v", err)
			}

			// stdout 模式输出
			if *stdout {
				data, err := json.Marshal(msg)
				if err != nil {
					log.Printf("[main] JSON 序列化消息失败: %v", err)
				} else {
					fmt.Println(string(data))
				}
			}

		case err, ok := <-errCh:
			if !ok {
				log.Println("[main] 错误通道已关闭")
				continue
			}
			log.Printf("[main] 消费者错误: %v", err)
			// 通过 Notifier 发送连接异常警告
			if notifyErr := nt.NotifyError("消费连接异常", err.Error()); notifyErr != nil {
				log.Printf("[main] 发送错误通知失败: %v", notifyErr)
			}
		}
	}

shutdown:
	// 优雅关闭
	log.Println("[main] 正在关闭 Web 服务器...")
	if ws != nil {
		if err := ws.Stop(); err != nil {
			log.Printf("[main] 关闭 Web 服务器失败: %v", err)
		}
	}
	log.Println("[main] 正在关闭消费者...")
	for _, ce := range consumers {
		if err := ce.Close(); err != nil {
			log.Printf("[main] 关闭 %s 消费者失败: %v", ce.name, err)
		}
	}
	log.Println("[main] 正在关闭异步通知...")
	asyncNt.Close()
	log.Println("[main] 正在关闭存储...")
	if err := store.Close(); err != nil {
		log.Printf("[main] 关闭存储失败: %v", err)
	}

	// 业务退出后触发全局 cancel（Windows 场景：确保 systray 也收到退出信号）
	cancel()
}

// logNotifier 是 Notifier 的日志实现，当通知被禁用时使用
type logNotifier struct{}

func (l *logNotifier) Notify(msg *model.Message) error {
	log.Printf("[notifier] [%s] %s: %s", msg.Level, msg.Title, truncate(msg.Content, 100))
	return nil
}

func (l *logNotifier) NotifyError(title string, body string) error {
	log.Printf("[notifier] [error] %s: %s", title, body)
	return nil
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

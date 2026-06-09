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
	"github.com/oliverxu/alertfly/internal/web"
)

func main() {
	// --- 命令行参数解析 ---
	configPath := flag.String("config", "./config.yaml", "配置文件路径")
	redisAddr := flag.String("redis-addr", "", "Redis 地址（覆盖配置文件）")
	kafkaBrokers := flag.String("kafka-brokers", "", "Kafka brokers，逗号分隔（覆盖配置文件）")
	stdout := flag.Bool("stdout", false, "启用标准输出模式")
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
	if cfg.Consumer.Type == "" {
		cfg.Consumer.Type = "redis"
	}

	// Web 配置默认值
	if cfg.Web.Port == 0 {
		cfg.Web.Port = 18080
	}

	// --- 初始化 Storage ---
	store, err := storage.NewSQLiteStorage(cfg.Storage.DBPath)
	if err != nil {
		log.Fatalf("[main] 初始化 SQLite 存储失败: %v", err)
	}
	defer store.Close()
	log.Println("[main] SQLite 存储初始化成功")

	// --- 初始化 Web 服务器 ---
	var ws *web.WebServer
	if cfg.Web.Enabled {
		ws = web.NewWebServer(cfg.Web.Port, *configPath, store, cfg)
		if err := ws.Start(); err != nil {
			log.Printf("[main] Web UI 启动失败: %v", err)
		}
	} else {
		log.Println("[main] Web UI 已禁用")
	}

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

	// --- 初始化并启动 Consumer（带重试） ---
	var cons consumer.Consumer
	{
		retryInterval := 5 * time.Second
		const maxRetryInterval = 60 * time.Second
		for {
			var createErr error
			switch cfg.Consumer.Type {
			case "redis":
				cons, createErr = consumer.NewRedisConsumer(&cfg.Redis)
				if createErr != nil {
					cons = nil
					createErr = fmt.Errorf("创建 Redis 消费者失败: %w", createErr)
				}
			case "kafka":
				cons, createErr = consumer.NewKafkaConsumer(&cfg.Kafka)
				if createErr != nil {
					cons = nil
					createErr = fmt.Errorf("创建 Kafka 消费者失败: %w", createErr)
				}
			default:
				log.Fatalf("[main] 不支持的消费者类型: %s", cfg.Consumer.Type)
			}

			if createErr == nil {
				if startErr := cons.Start(ctx); startErr != nil {
					cons.Close()
					cons = nil
					createErr = fmt.Errorf("启动消费者失败: %w", startErr)
				}
			}

			if createErr == nil {
				log.Printf("[main] 消费者已启动，类型: %s", cfg.Consumer.Type)
				break
			}

			log.Printf("[main] %v，%v 后重试...", createErr, retryInterval)
			if notifyErr := nt.NotifyError("消费者启动失败", createErr.Error()); notifyErr != nil {
				log.Printf("[main] 发送错误通知失败: %v", notifyErr)
			}

			select {
			case <-ctx.Done():
				log.Println("[main] 收到退出信号，停止重试")
				goto shutdown
			case <-time.After(retryInterval):
			}

			retryInterval *= 2
			if retryInterval > maxRetryInterval {
				retryInterval = maxRetryInterval
			}
		}
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
	log.Println("[main] AlertFly 主循环启动，等待消息...")
	for {
		select {
		case <-ctx.Done():
			log.Println("[main] 主循环退出")
			goto shutdown
		case msg, ok := <-cons.Messages():
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

			// 通过 Notifier 弹窗通知
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

		case err, ok := <-cons.Errors():
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
	if cons != nil {
		if err := cons.Close(); err != nil {
			log.Printf("[main] 关闭消费者失败: %v", err)
		}
	}
	log.Println("[main] 正在关闭存储...")
	if err := store.Close(); err != nil {
		log.Printf("[main] 关闭存储失败: %v", err)
	}
	log.Println("[main] AlertFly 已退出")
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

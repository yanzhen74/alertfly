// +build ignore

// Redis Mock Send Tool
// 直接运行: go run scripts/redis_mock_send.go
//
// 功能：
//   - 连接到本地 Redis 服务器
//   - 支持 PubSub 模式发布报警消息
//   - 支持 Stream 模式写入报警消息
//   - 发送多条不同级别的测试报警
//
// 用法：
//   go run scripts/redis_mock_send.go -mode pubsub
//   go run scripts/redis_mock_send.go -mode stream
//   go run scripts/redis_mock_send.go -mode both
//   go run scripts/redis_mock_send.go -addr localhost:6379 -channel alertfly:alerts -mode pubsub
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
)

// AlertMessage 模拟报警消息结构
type AlertMessage struct {
	ID      int64  `json:"id"`
	Source  string `json:"source"`
	Topic   string `json:"topic"`
	Level   string `json:"level"`
	SubType string `json:"subtype"`
	Sender  string `json:"sender"`
	Mission string `json:"mission"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func main() {
	addr := flag.String("addr", "localhost:6379", "Redis server address")
	password := flag.String("password", "", "Redis password")
	db := flag.Int("db", 0, "Redis database number")
	channel := flag.String("channel", "alerts", "PubSub channel name")
	stream := flag.String("stream", "alert_stream", "Stream name")
	mode := flag.String("mode", "both", "Send mode: pubsub, stream, or both")
	count := flag.Int("count", 3, "Number of messages per level to send")
	flag.Parse()

	if *mode != "pubsub" && *mode != "stream" && *mode != "both" {
		fmt.Fprintf(os.Stderr, "invalid mode: %s (must be pubsub, stream, or both)\n", *mode)
		os.Exit(1)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     *addr,
		Password: *password,
		DB:       *db,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 测试连接
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to Redis at %s: %v", *addr, err)
	}
	fmt.Printf("Connected to Redis at %s\n", *addr)

	// 生成测试报警消息
	levels := []string{"info", "warn", "error"}
	alerts := []AlertMessage{
		{
			Source:  "mock-sender", SubType: "cpu_alert", Sender: "prometheus",
			Mission: "infra-monitor",
			Title:   "CPU usage over threshold",
			Content: "CPU utilization has exceeded the defined threshold for 5 consecutive minutes",
		},
		{
			Source:  "mock-sender", SubType: "disk_alert", Sender: "node-exporter",
			Mission: "disk-monitor",
			Title:   "Disk space running low",
			Content: "Available disk space on /data partition is below 15%",
		},
		{
			Source:  "mock-sender", SubType: "memory_alert", Sender: "grafana",
			Mission: "memory-monitor",
			Title:   "Memory usage critical",
			Content: "Memory usage has reached 92% and continues to rise",
		},
	}

	var totalSent int64

	for i := 0; i < *count; i++ {
		for li, level := range levels {
			alert := alerts[li]
			alert.ID = time.Now().UnixNano()
			alert.Topic = *channel
			alert.Level = level

			data, err := json.Marshal(alert)
			if err != nil {
				log.Printf("failed to marshal alert: %v", err)
				continue
			}

			if *mode == "pubsub" || *mode == "both" {
				result, err := client.Publish(ctx, *channel, string(data)).Result()
				if err != nil {
					log.Printf("[PUBSUB] failed to publish: %v", err)
				} else {
					totalSent++
					fmt.Printf("[PUBSUB] ✓ published %-5s alert to channel '%s' (subscribers: %d)\n", level, *channel, result)
				}
			}

			if *mode == "stream" || *mode == "both" {
				streamValues := map[string]interface{}{
					"id":      fmt.Sprintf("%d", alert.ID),
					"source":  alert.Source,
					"topic":   alert.Topic,
					"level":   alert.Level,
					"subtype": alert.SubType,
					"sender":  alert.Sender,
					"mission": alert.Mission,
					"title":   alert.Title,
					"content": alert.Content,
				}

				streamID, err := client.XAdd(ctx, &redis.XAddArgs{
					Stream: *stream,
					Values: streamValues,
				}).Result()
				if err != nil {
					log.Printf("[STREAM] failed to XAdd: %v", err)
				} else {
					totalSent++
					fmt.Printf("[STREAM] ✓ added %-5s alert to stream '%s' (id: %s)\n", level, *stream, streamID)
				}
			}

			// 稍微延迟，避免消息堆积太快
			time.Sleep(200 * time.Millisecond)
		}
	}

	fmt.Printf("\nDone! Total messages sent: %d\n", totalSent)
}

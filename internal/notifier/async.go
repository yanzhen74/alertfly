package notifier

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/oliverxu/alertfly/internal/model"
)

// AsyncNotifier 异步通知包装器，带限流和合并能力。
// 将同步的 Notify 调用改为非阻塞投递到内部 channel，
// 由独立 goroutine 消费并执行限流/合并逻辑。
type AsyncNotifier struct {
	inner      Notifier                      // 内部通知器（linuxNotifier / windowsNotifier / logNotifier）
	trayNotify func(title, content string)   // 系统托盘通知回调
	ch         chan *model.Message            // 通知消息队列
	minGap     time.Duration                 // 两次通知最小间隔
	maxBatch   int                           // 积压超过此数时合并为摘要通知
	stopCh     chan struct{}                  // 关闭信号
	done       chan struct{}                  // goroutine 退出确认
	mu         sync.Mutex                    // 保护 closed 字段
	closed     bool                          // 是否已关闭
}

const (
	defaultMinGap   = 1 * time.Second  // 两次通知最小间隔
	defaultMaxBatch = 3                 // 积压超过 3 条时合并
	defaultChanSize = 100               // channel 缓冲区大小
)

// NewAsyncNotifier 创建异步通知包装器。
// trayNotify 为系统托盘通知回调，可为 nil（仅使用 inner 通知器）。
func NewAsyncNotifier(inner Notifier, trayNotify func(title, content string)) *AsyncNotifier {
	return &AsyncNotifier{
		inner:      inner,
		trayNotify: trayNotify,
		ch:         make(chan *model.Message, defaultChanSize),
		minGap:     defaultMinGap,
		maxBatch:   defaultMaxBatch,
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Start 启动消费 goroutine。传入 appCtx 用于程序退出时自动终止。
func (a *AsyncNotifier) Start(appCtx context.Context) {
	go a.processLoop(appCtx)
}

// Notify 非阻塞投递消息到通知队列。
// 队列已满或已关闭时丢弃通知并记录日志，返回 nil（不视为错误）。
// 注意：消息的存储由主循环同步完成，此处仅影响通知展示。
func (a *AsyncNotifier) Notify(msg *model.Message) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		log.Printf("[notifier] 通知队列已关闭，丢弃通知: [%s] %s", msg.Level, msg.Title)
		return nil
	}
	a.mu.Unlock()

	select {
	case a.ch <- msg:
		return nil
	default:
		log.Printf("[notifier] 通知队列已满(%d)，丢弃通知: [%s] %s",
			defaultChanSize, msg.Level, msg.Title)
		return nil
	}
}

// NotifyError 错误通知直接转发到 inner，不限流、不入队。
// 系统错误通知需要立即展示，不参与限流逻辑。
func (a *AsyncNotifier) NotifyError(title, body string) error {
	return a.inner.NotifyError(title, body)
}

// Close 优雅关闭：停止消费 goroutine，丢弃队列中未处理的消息。
// 调用后 Notify 将丢弃所有新消息。
func (a *AsyncNotifier) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	a.mu.Unlock()
	close(a.stopCh)
	<-a.done // 等待 goroutine 退出
}

// processLoop 消费 goroutine 主逻辑：
//  1. 从 channel 取消息
//  2. 强制 minGap 间隔（限流）
//  3. 检查队列积压深度，决定单独发送还是合并摘要
func (a *AsyncNotifier) processLoop(appCtx context.Context) {
	defer close(a.done)
	var lastNotify time.Time

	for {
		select {
		case <-a.stopCh:
			// 丢弃队列中剩余消息
			for len(a.ch) > 0 {
				<-a.ch
			}
			return
		case <-appCtx.Done():
			for len(a.ch) > 0 {
				<-a.ch
			}
			return
		case msg := <-a.ch:
			// --- 限流：强制两次通知最小间隔 ---
			gapRemaining := a.minGap - time.Since(lastNotify)
			if gapRemaining > 0 {
				timer := time.NewTimer(gapRemaining)
				select {
				case <-a.stopCh:
					timer.Stop()
					return
				case <-appCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}

			// --- 合流决策：检查队列积压 ---
			queued := len(a.ch)

			if queued >= a.maxBatch {
				// 积压超过阈值 → 合并为摘要通知
				batch := []*model.Message{msg}
				for len(a.ch) > 0 {
					batch = append(batch, <-a.ch)
				}
				summary := a.createSummary(batch)
				if err := a.inner.Notify(summary); err != nil {
					log.Printf("[notifier] 发送摘要通知失败: %v", err)
				}
				if a.trayNotify != nil {
					a.trayNotify(summary.Title, summary.Content)
				}
				log.Printf("[notifier] 合并 %d 条消息为摘要通知（最高级别: %s）",
					len(batch), summary.Level)
			} else {
				// 正常模式 → 单条通知
				if err := a.inner.Notify(msg); err != nil {
					log.Printf("[notifier] 发送通知失败: %v", err)
				}
				if a.trayNotify != nil {
					a.trayNotify(msg.Title, msg.Content)
				}
			}

			lastNotify = time.Now()
		}
	}
}

// createSummary 将一批消息合并为一条摘要通知
func (a *AsyncNotifier) createSummary(batch []*model.Message) *model.Message {
	highest := highestLevel(batch)
	count := len(batch)

	title := fmt.Sprintf("收到 %d 条新告警", count)
	content := fmt.Sprintf("最高级别: %s，请查看详情", highest)

	return &model.Message{
		Level:      highest,
		Title:      title,
		Content:    content,
		Source:     "system",
		ReceivedAt: time.Now(),
	}
}

// highestLevel 返回消息列表中最高严重级别
func highestLevel(messages []*model.Message) string {
	priority := map[string]int{
		"error": 3,
		"warn":  2,
		"info":  1,
	}
	maxP := 0
	result := "info"
	for _, m := range messages {
		p, ok := priority[strings.ToLower(m.Level)]
		if !ok {
			p = 0
		}
		if p > maxP {
			maxP = p
			result = strings.ToLower(m.Level)
		}
	}
	return result
}
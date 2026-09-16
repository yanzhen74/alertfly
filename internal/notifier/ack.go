package notifier

import (
	"sync"
	"time"

	"github.com/oliverxu/alertfly/internal/logger"
	"github.com/oliverxu/alertfly/internal/model"
	"github.com/oliverxu/alertfly/internal/sound"
)

// AckConfig 未确认告警管理器的配置。
type AckConfig struct {
	// PersistLevel 触发持久化告警（循环声音）的最低级别：
	//   ""       禁用持久化告警
	//   "warn"   warn 及以上触发
	//   "error"  仅 error 触发
	PersistLevel string
	// SoundFile 循环播放的声音文件路径，空=使用内嵌声音。
	SoundFile string
	// SoundLoopInterval 循环播放间隔，<=0 时默认 5s。
	SoundLoopInterval time.Duration
}

// Acknowledger 管理"未确认告警"状态。
//
// 触发流程：当收到级别 >= PersistLevel 的消息时进入激活态，
// 启动声音循环，并将消息加入 pending 列表；
// 用户通过任一确认入口（托盘菜单/Web UI/未来的持久化弹窗按钮）
// 调用 Acknowledge 后停止声音、清空 pending。
//
// 该类型并发安全。
type Acknowledger struct {
	mu sync.Mutex

	triggerLevel  int           // 触发的最低级别优先级，0=禁用
	soundFile     string
	soundInterval time.Duration

	pending []*model.Message // 待确认的告警（按到达顺序）
	active  bool             // 是否处于激活态（声音循环中）

	// 阶段 2 预留：持久化弹窗显示/隐藏回调（当前均为 nil）
	onActivate   func(msgs []*model.Message)
	onDeactivate func()
}

// NewAcknowledger 创建 Acknowledger；cfg.PersistLevel 为空时返回禁用状态的实例
// （OnMessage 不会触发任何动作，Acknowledge 也是空操作）。
func NewAcknowledger(cfg AckConfig) *Acknowledger {
	a := &Acknowledger{}
	a.applyConfig(cfg)
	return a
}

// applyConfig 应用配置（不加锁，仅内部使用）。
func (a *Acknowledger) applyConfig(cfg AckConfig) {
	a.triggerLevel = levelPriority(cfg.PersistLevel)
	a.soundFile = cfg.SoundFile
	a.soundInterval = cfg.SoundLoopInterval
	if a.soundInterval <= 0 {
		a.soundInterval = 5 * time.Second
	}
}

// UpdateConfig 热重载配置。若新配置禁用持久化告警且当前处于激活态，
// 则立即停止声音循环并清空 pending。
func (a *Acknowledger) UpdateConfig(cfg AckConfig) {
	a.mu.Lock()
	a.applyConfig(cfg)
	shouldStop := a.active && a.triggerLevel <= 0
	if shouldStop {
		a.active = false
		a.pending = nil
	}
	a.mu.Unlock()

	if shouldStop {
		sound.StopLoop()
		if a.onDeactivate != nil {
			a.onDeactivate()
		}
		logger.Info("[ack] 持久化告警已禁用，停止循环声音")
	}
}

// SetCallbacks 注册激活/去激活回调（阶段 2 用于持久化弹窗显示/隐藏）。
// 传入 nil 表示清除。回调在锁外调用，可安全执行阻塞操作。
func (a *Acknowledger) SetCallbacks(onActivate func(msgs []*model.Message), onDeactivate func()) {
	a.mu.Lock()
	a.onActivate = onActivate
	a.onDeactivate = onDeactivate
	a.mu.Unlock()
}

// OnMessage 由 AsyncNotifier 在每条通知发送后调用。
// 级别不足或已禁用时立即返回；否则加入 pending，若首次进入激活态则启动声音循环。
func (a *Acknowledger) OnMessage(msg *model.Message) {
	if msg == nil {
		return
	}
	a.mu.Lock()
	if a.triggerLevel <= 0 {
		a.mu.Unlock()
		return
	}
	if levelPriority(msg.Level) < a.triggerLevel {
		a.mu.Unlock()
		return
	}
	a.pending = append(a.pending, msg)
	firstActivate := !a.active
	a.active = true
	soundFile := a.soundFile
	interval := a.soundInterval
	snapshot := append([]*model.Message(nil), a.pending...)
	onActivate := a.onActivate
	a.mu.Unlock()

	if firstActivate {
		sound.StartLoop(soundFile, interval)
		if onActivate != nil {
			onActivate(snapshot)
		}
		logger.Info("[ack] 触发持久化告警，级别=%s，标题=%s（声音循环已启动，间隔=%s）",
			msg.Level, msg.Title, interval)
	} else {
		// 已在激活态，仅更新弹窗内容（如果阶段 2 已接入）
		if onActivate != nil {
			onActivate(snapshot)
		}
		logger.Debug("[ack] 追加待确认告警: [%s] %s（当前 pending=%d）",
			msg.Level, msg.Title, len(snapshot))
	}
}

// Acknowledge 用户确认所有待处理告警：停止声音循环、清空 pending。
// 未处于激活态时为空操作。可安全并发调用。
func (a *Acknowledger) Acknowledge() {
	a.mu.Lock()
	if !a.active {
		a.mu.Unlock()
		return
	}
	a.active = false
	count := len(a.pending)
	a.pending = nil
	onDeactivate := a.onDeactivate
	a.mu.Unlock()

	sound.StopLoop()
	if onDeactivate != nil {
		onDeactivate()
	}
	logger.Info("[ack] 用户确认告警，已停止循环声音（清空 %d 条 pending）", count)
}

// Active 报告当前是否处于激活态（声音循环中）。
func (a *Acknowledger) Active() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}

// PendingCount 返回待确认告警数量。
func (a *Acknowledger) PendingCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending)
}

// Pending 返回待确认告警的快照（深副本，可安全遍历与修改）。
func (a *Acknowledger) Pending() []*model.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*model.Message, 0, len(a.pending))
	for _, m := range a.pending {
		if m == nil {
			continue
		}
		cp := *m
		out = append(out, &cp)
	}
	return out
}

// Enabled 报告持久化告警功能是否已启用（triggerLevel > 0）。
func (a *Acknowledger) Enabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.triggerLevel > 0
}

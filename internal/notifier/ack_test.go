package notifier

import (
	"testing"
	"time"

	"github.com/oliverxu/alertfly/internal/model"
)

func TestAcknowledger_DisabledByDefault(t *testing.T) {
	a := NewAcknowledger(AckConfig{PersistLevel: ""})
	if a.Enabled() {
		t.Errorf("PersistLevel 为空时应为禁用状态")
	}
	a.OnMessage(&model.Message{Level: "error", Title: "x"})
	if a.Active() {
		t.Errorf("禁用状态下不应激活")
	}
	if a.PendingCount() != 0 {
		t.Errorf("禁用状态下 pending 应为 0")
	}
}

func TestAcknowledger_TriggersAtLevel(t *testing.T) {
	a := NewAcknowledger(AckConfig{
		PersistLevel:      "warn",
		SoundLoopInterval: 1 * time.Hour, // 避免测试中真的循环
	})
	defer a.Acknowledge()

	// info 级别不触发
	a.OnMessage(&model.Message{Level: "info", Title: "info-msg"})
	if a.Active() {
		t.Errorf("info 级别不应触发持久化告警")
	}

	// warn 级别触发
	a.OnMessage(&model.Message{Level: "warn", Title: "warn-msg"})
	if !a.Active() {
		t.Errorf("warn 级别应触发持久化告警")
	}
	if a.PendingCount() != 1 {
		t.Errorf("pending 应为 1，实际 %d", a.PendingCount())
	}

	// error 级别继续追加
	a.OnMessage(&model.Message{Level: "error", Title: "err-msg"})
	if a.PendingCount() != 2 {
		t.Errorf("pending 应为 2，实际 %d", a.PendingCount())
	}
}

func TestAcknowledger_AcknowledgeClearsState(t *testing.T) {
	a := NewAcknowledger(AckConfig{
		PersistLevel:      "error",
		SoundLoopInterval: 1 * time.Hour,
	})
	a.OnMessage(&model.Message{Level: "error", Title: "e1"})
	a.OnMessage(&model.Message{Level: "error", Title: "e2"})
	if !a.Active() || a.PendingCount() != 2 {
		t.Fatalf("前置状态错误: active=%v, count=%d", a.Active(), a.PendingCount())
	}

	a.Acknowledge()
	if a.Active() {
		t.Errorf("Acknowledge 后应退出激活态")
	}
	if a.PendingCount() != 0 {
		t.Errorf("Acknowledge 后 pending 应清空")
	}

	// 二次 Acknowledge 不应 panic
	a.Acknowledge()
}

func TestAcknowledger_UpdateConfigDisableStopsActive(t *testing.T) {
	a := NewAcknowledger(AckConfig{
		PersistLevel:      "warn",
		SoundLoopInterval: 1 * time.Hour,
	})
	a.OnMessage(&model.Message{Level: "warn", Title: "x"})
	if !a.Active() {
		t.Fatalf("前置状态错误: 应为激活")
	}

	// 禁用持久化告警，应立即停止
	a.UpdateConfig(AckConfig{PersistLevel: "", SoundLoopInterval: 1 * time.Hour})
	if a.Active() {
		t.Errorf("禁用后应退出激活态")
	}
	if a.Enabled() {
		t.Errorf("Enabled 应返回 false")
	}
	if a.PendingCount() != 0 {
		t.Errorf("禁用时应清空 pending")
	}
}

func TestAcknowledger_CallbacksInvoked(t *testing.T) {
	a := NewAcknowledger(AckConfig{
		PersistLevel:      "error",
		SoundLoopInterval: 1 * time.Hour,
	})
	var activatedCount int
	var deactivatedCount int
	a.SetCallbacks(
		func(msgs []*model.Message) { activatedCount++ },
		func() { deactivatedCount++ },
	)

	a.OnMessage(&model.Message{Level: "error", Title: "e1"})
	a.OnMessage(&model.Message{Level: "error", Title: "e2"}) // 追加也会调用 onActivate
	if activatedCount != 2 {
		t.Errorf("onActivate 应被调用 2 次（首次激活 + 追加），实际 %d", activatedCount)
	}

	a.Acknowledge()
	if deactivatedCount != 1 {
		t.Errorf("onDeactivate 应被调用 1 次，实际 %d", deactivatedCount)
	}
}

func TestAcknowledger_PendingSnapshotIsCopy(t *testing.T) {
	a := NewAcknowledger(AckConfig{
		PersistLevel:      "error",
		SoundLoopInterval: 1 * time.Hour,
	})
	defer a.Acknowledge()

	a.OnMessage(&model.Message{Level: "error", Title: "e1"})
	snap := a.Pending()
	snap[0].Title = "mutated"

	original := a.Pending()
	if original[0].Title == "mutated" {
		t.Errorf("Pending 应返回副本，不允许外部修改影响内部状态")
	}
}

func TestAcknowledger_NilMessageIgnored(t *testing.T) {
	a := NewAcknowledger(AckConfig{PersistLevel: "warn", SoundLoopInterval: 1 * time.Hour})
	defer a.Acknowledge()
	a.OnMessage(nil)
	if a.Active() {
		t.Errorf("nil 消息不应触发激活")
	}
}

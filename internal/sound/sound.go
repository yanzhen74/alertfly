package sound

import (
	_ "embed"
	"log"
	"os"
	"sync"
	"time"
)

// defaultWAV 内嵌的默认报警声音（800Hz 双音 beep，约 0.5s）
//
//go:embed alert.wav
var defaultWAV []byte

// Play 播放报警声音。
// customFile 非空时优先读取外部文件；否则使用内嵌声音。
// 播放失败仅记录日志，不影响业务流程。
func Play(customFile string) {
	var data []byte
	if customFile != "" {
		b, err := os.ReadFile(customFile)
		if err != nil {
			log.Printf("[sound] 读取声音文件失败 %s: %v，使用内嵌声音", customFile, err)
			data = defaultWAV
		} else {
			data = b
		}
	} else {
		data = defaultWAV
	}

	if err := playWAV(data); err != nil {
		log.Printf("[sound] 播放声音失败: %v", err)
	}
}

// --- 循环播放（用于持久化报警，直到用户确认）---

var (
	loopMu     sync.Mutex
	loopStopCh chan struct{}
	loopActive bool
)

// StartLoop 启动循环播放（异步、幂等）。已处于循环中时仅更新参数不重启。
// customFile 意义同 Play；interval 为两次播放之间的间隔，默认 5s。
func StartLoop(customFile string, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	loopMu.Lock()
	if loopActive {
		loopMu.Unlock()
		return
	}
	loopActive = true
	loopStopCh = make(chan struct{})
	stopCh := loopStopCh
	loopMu.Unlock()

	go func() {
		// 立即播放一次，避免首周期静默
		Play(customFile)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				Play(customFile)
			}
		}
	}()
}

// StopLoop 停止循环播放。未处于循环中时为空操作。
func StopLoop() {
	loopMu.Lock()
	if !loopActive {
		loopMu.Unlock()
		return
	}
	loopActive = false
	close(loopStopCh)
	loopStopCh = nil
	loopMu.Unlock()
}

// Looping 报告当前是否处于循环播放状态。
func Looping() bool {
	loopMu.Lock()
	defer loopMu.Unlock()
	return loopActive
}

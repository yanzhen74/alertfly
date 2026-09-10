package sound

import (
	_ "embed"
	"log"
	"os"
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

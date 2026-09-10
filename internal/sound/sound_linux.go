//go:build linux

package sound

import (
	"fmt"
	"os"
	"os/exec"
)

// playWAV 在 Linux 上通过外部命令播放 WAV 数据。
// 依次尝试 paplay（PulseAudio）→ aplay（ALSA），任一成功即返回。
func playWAV(data []byte) error {
	// 写入临时文件（外部命令需要文件路径）
	tmp, err := os.CreateTemp("", "alertfly-*.wav")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// 尝试 paplay（PulseAudio / PipeWire）
	if path, err := exec.LookPath("paplay"); err == nil {
		cmd := exec.Command(path, tmp.Name())
		if err := cmd.Run(); err == nil {
			return nil
		}
		// paplay 失败，继续尝试 aplay
	}

	// 尝试 aplay（ALSA）
	if path, err := exec.LookPath("aplay"); err == nil {
		return exec.Command(path, "-q", tmp.Name()).Run()
	}

	return fmt.Errorf("no sound player found (paplay/aplay)")
}

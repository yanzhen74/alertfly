//go:build windows

package sound

import (
	"syscall"
	"unsafe"
)

var (
	winmm         = syscall.NewLazyDLL("winmm.dll")
	playSoundProc = winmm.NewProc("PlaySoundW")
)

const (
	sndMemory = 0x0004 // 从内存播放
	sndAsync  = 0x0001 // 异步播放，不阻塞
)

// playWAV 在 Windows 上通过 winmm.PlaySound 从内存播放 WAV 数据。
func playWAV(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	// PlaySoundW(pszSound, hmod, fdwSound)
	// hmod = 0（非资源），fdwSound = SND_MEMORY | SND_ASYNC
	ret, _, err := playSoundProc.Call(
		uintptr(unsafe.Pointer(&data[0])),
		0,
		uintptr(sndMemory|sndAsync),
	)
	if ret == 0 {
		return err
	}
	return nil
}

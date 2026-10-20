package stream

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// H.264 encoders, in order of preference when RTCConfig.Encoder is "auto": the GPU's when it
// works (VAAPI on Intel / AMD, NVENC on NVIDIA), otherwise libx264 on the CPU.
const (
	EncAuto  = "auto"
	EncVAAPI = "vaapi"
	EncNVENC = "nvenc"
	EncX264  = "x264"
)

// vaapiDevice is the render node VAAPI encodes on.
var vaapiDevice = "/dev/dri/renderD128"

var (
	encOnce sync.Once
	encUsed string
)

// h264Encoder returns the encoder to use for the setting (auto / vaapi / nvenc / x264). A GPU
// encoder is used only after a short test encode works on this machine; the choice is made
// once and logged.
func h264Encoder(want string) string {
	encOnce.Do(func() {
		encUsed = pickEncoder(want)
		log.Printf("rtc: H.264 encoder %s", encUsed)
	})
	return encUsed
}

func pickEncoder(want string) string {
	if want == EncX264 {
		return EncX264
	}
	cands := []string{EncVAAPI, EncNVENC}
	if want == EncVAAPI || want == EncNVENC {
		cands = []string{want}
	}
	for _, e := range cands {
		if encoderWorks(e) {
			return e
		}
		if want != EncAuto && want != "" {
			log.Printf("rtc: %s encoder not usable here, using x264", e)
		}
	}
	return EncX264
}

// encoderWorks encodes a fraction of a second of a test picture with the encoder.
func encoderWorks(enc string) bool {
	if enc == EncVAAPI {
		if _, err := os.Stat(vaapiDevice); err != nil {
			return false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=30", "-t", "0.3"}
	args = append(args, h264Args(enc, 1000, 30)...)
	args = append(args, "-f", "null", "-")
	return exec.CommandContext(ctx, "ffmpeg", args...).Run() == nil
}

// h264Args are the ffmpeg options after the input that encode for WebRTC: constrained
// baseline (every browser decodes it), no B-frames, an access unit delimiter before each frame
// (pumpH264 cuts the stream there), parameter sets repeated at every keyframe (a viewer joining
// later can decode), a keyframe at least every 10 s, and at most kbps.
func h264Args(enc string, kbps, fps int) []string {
	rate := strconv.Itoa(kbps) + "k"
	gop := strconv.Itoa(fps * 10)
	even := "crop=trunc(iw/2)*2:trunc(ih/2)*2"
	switch enc {
	case EncVAAPI:
		return []string{"-vaapi_device", vaapiDevice, "-vf", even + ",format=nv12,hwupload",
			"-c:v", "h264_vaapi", "-profile:v", "constrained_baseline", "-bf", "0", "-rc_mode", "VBR",
			"-b:v", rate, "-maxrate", rate, "-g", gop, "-aud", "1", "-bsf:v", "dump_extra=freq=keyframe"}
	case EncNVENC:
		return []string{"-vf", even + ",format=yuv420p", "-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ull",
			"-zerolatency", "1", "-profile:v", "baseline", "-bf", "0", "-rc", "vbr", "-cq", "23",
			"-b:v", rate, "-maxrate", rate, "-bufsize", strconv.Itoa(kbps/2) + "k", "-g", gop, "-aud", "1",
			"-bsf:v", "dump_extra=freq=keyframe"}
	}
	return []string{"-vf", even + ",format=yuv420p", "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-profile:v", "baseline", "-crf", "23", "-maxrate", rate, "-bufsize", strconv.Itoa(kbps/2) + "k",
		"-g", gop, "-bf", "0", "-x264-params", "aud=1:repeat-headers=1"}
}

// ParseEncoder checks an -encoder value.
func ParseEncoder(s string) (string, bool) {
	s = strings.ToLower(s)
	switch s {
	case "", EncAuto:
		return EncAuto, true
	case EncVAAPI, EncNVENC, EncX264:
		return s, true
	}
	return "", false
}

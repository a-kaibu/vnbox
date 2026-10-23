package stream

// WebRTC delivery: H.264 (or VP8 for browsers without an H.264 decoder) + Opus, one encoder
// per game shared by every viewer, input over a data channel. Signalling is a single HTTP
// exchange (POST the offer, get the answer after ICE gathering), so it works through the
// portal's normal authentication.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"

	"github.com/a-kaibu/vnbox/internal/x11"
)

// RTCConfig is how peers reach this server. The zero value works on a LAN.
type RTCConfig struct {
	ICEServers []webrtc.ICEServer // STUN / TURN for viewers behind NAT
	PublicIPs  []string           // announce these instead of the local addresses (1:1 NAT, port forward)
	Port       int                // single UDP and TCP port for all media (0 = random UDP ports, no TCP)
	Bitrate    int                // video kbit/s ceiling (default 3000); each viewer's connection may lower it (abr.go)
	Encoder    string             // H.264 encoder: auto (GPU when it works) / vaapi / nvenc / x264 (encoder.go)
}

var (
	rtcMu  sync.Mutex
	rtcCfg RTCConfig
	rtcAPI *webrtc.API
)

// ConfigureRTC sets the WebRTC network settings; call once before serving.
func ConfigureRTC(c RTCConfig) error {
	rtcMu.Lock()
	defer rtcMu.Unlock()
	if c.Bitrate == 0 {
		c.Bitrate = 3000
	}
	se := webrtc.SettingEngine{}
	if len(c.PublicIPs) > 0 {
		se.SetNAT1To1IPs(c.PublicIPs, webrtc.ICECandidateTypeHost)
	}
	if c.Port > 0 {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{Port: c.Port})
		if err != nil {
			return fmt.Errorf("rtc udp port: %w", err)
		}
		se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udp))
		tcp, err := net.ListenTCP("tcp", &net.TCPAddr{Port: c.Port})
		if err != nil {
			return fmt.Errorf("rtc tcp port: %w", err)
		}
		se.SetICETCPMux(webrtc.NewICETCPMux(nil, tcp, 64))
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6, webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6})
	}
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return err
	}
	rtcAPI = webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se))
	rtcCfg = c
	return nil
}

func api() (*webrtc.API, RTCConfig) {
	rtcMu.Lock()
	defer rtcMu.Unlock()
	if rtcAPI == nil {
		rtcMu.Unlock()
		ConfigureRTC(RTCConfig{})
		rtcMu.Lock()
	}
	return rtcAPI, rtcCfg
}

// ParseICE turns "stun:host:3478" or "turn:user:pass@host:3478" into an ICE server.
func ParseICE(s string) (webrtc.ICEServer, error) {
	u, err := url.Parse(strings.Replace(strings.Replace(s, "turn:", "turn://", 1), "turns:", "turns://", 1))
	if err != nil || !strings.HasPrefix(s, "stun:") && u.Host == "" {
		return webrtc.ICEServer{}, fmt.Errorf("bad ICE server %q", s)
	}
	if strings.HasPrefix(s, "stun:") {
		return webrtc.ICEServer{URLs: []string{s}}, nil
	}
	srv := webrtc.ICEServer{URLs: []string{u.Scheme + ":" + u.Host + func() string {
		if u.RawQuery != "" {
			return "?" + u.RawQuery
		}
		return ""
	}()}}
	if u.User != nil {
		srv.Username = u.User.Username()
		srv.Credential, _ = u.User.Password()
	}
	return srv, nil
}

// hub is one encoder pipeline (video codec + Opus) fanned out to every viewer.
type hub struct {
	g     *Game
	codec string // "h264" or "vp8"
	video *webrtc.TrackLocalStaticSample
	audio *webrtc.TrackLocalStaticSample

	mu       sync.Mutex
	peers    int
	cancel   context.CancelFunc
	idle     *time.Timer
	kick     chan struct{} // restart the encoder now so the next frame is a keyframe
	lastKick time.Time
	ctl      *rateCtl // the video rate the viewer's connection allows (abr.go)
	rate     int      // the rate the encoder runs at (kbit/s)
}

// retarget restarts the encoder when the rate changed (h.mu held): x264's rate cannot be
// changed while it runs.
func (h *hub) retarget() {
	r := h.ctl.kbps()
	if r == h.rate {
		return
	}
	log.Printf("rtc %s: video %d → %d kbit/s", h.g.Display, h.rate, r)
	h.rate = r
	h.lastKick = time.Now()
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

func (h *hub) currentRate() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rate = h.ctl.kbps()
	return h.rate
}

// keyframe asks for an IDR frame (new viewer, or a viewer reported loss with PLI/FIR).
// ffmpeg cannot be told to emit one, so the encoder is restarted; at most every 3 s.
func (h *hub) keyframe() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.lastKick) < 3*time.Second {
		return
	}
	h.lastKick = time.Now()
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

func (g *Game) hub(codec string) *hub {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.hubs == nil {
		g.hubs = map[string]*hub{}
	}
	if h := g.hubs[codec]; h != nil {
		return h
	}
	mime := webrtc.MimeTypeH264
	if codec == "vp8" {
		mime = webrtc.MimeTypeVP8
	}
	v, _ := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: mime}, "video", "vnbox")
	a, _ := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "vnbox")
	_, cfg := api()
	h := &hub{g: g, codec: codec, video: v, audio: a, kick: make(chan struct{}, 1), ctl: newRateCtl(cfg.Bitrate, time.Now())}
	g.hubs[codec] = h
	return h
}

// join starts the encoders for the first viewer; leave stops them 20 s after the last one.
func (h *hub) join() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.peers++
	if h.idle != nil {
		h.idle.Stop()
		h.idle = nil
	}
	if h.cancel != nil { // already streaming: the new viewer needs a keyframe now
		h.lastKick = time.Now()
		select {
		case h.kick <- struct{}{}:
		default:
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.ctl = newRateCtl(h.ctl.ceiling, time.Now()) // a new connection starts at the ceiling again
	go h.runVideo(ctx)
	if h.g.Sink != "" {
		go h.runAudio(ctx)
	}
}

func (h *hub) leave() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.peers--
	if h.peers > 0 || h.cancel == nil {
		return
	}
	h.idle = time.AfterFunc(20*time.Second, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.peers == 0 && h.cancel != nil {
			h.cancel()
			h.cancel = nil
		}
	})
}

// runVideo encodes the game window; when the window moves or changes size it restarts ffmpeg
// (the next keyframe carries the new resolution).
func (h *hub) runVideo(ctx context.Context) {
	for ctx.Err() == nil {
		win, ok := x11.Display(h.g.Display).Largest()
		if !ok || win.Area() <= 1 {
			sleep(ctx, time.Second)
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		go func() { // restart on a keyframe request or when the window moves / changes size
			for wctx.Err() == nil {
				select {
				case <-h.kick:
					cancel()
					return
				case <-wctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				if w, ok := x11.Display(h.g.Display).Largest(); ok && (w.W != win.W || w.H != win.H || w.X != win.X || w.Y != win.Y) {
					cancel()
					return
				}
			}
		}()
		if err := h.encode(wctx, win); err != nil && wctx.Err() == nil {
			log.Printf("rtc video %s: %v", h.g.Display, err)
			sleep(ctx, 2*time.Second)
		}
		cancel()
	}
}

func (h *hub) encode(ctx context.Context, win x11.Window) error {
	_, cfg := api()
	fps := h.g.FPS
	if fps < 24 {
		fps = 30 // WebRTC only sends what changed, so a higher rate costs little on still screens
	}
	kbps := h.currentRate()
	rate := strconv.Itoa(kbps) + "k"
	args := []string{"-v", "error", "-f", "x11grab", "-draw_mouse", "0", "-framerate", strconv.Itoa(fps),
		"-video_size", fmt.Sprintf("%dx%d", win.W, win.H), "-i", fmt.Sprintf("%s+%d,%d", h.g.Display, win.X, win.Y)}
	if h.codec == "vp8" {
		args = append(args, "-vf", "crop=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p",
			"-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-lag-in-frames", "0",
			"-b:v", rate, "-maxrate", rate, "-crf", "10", "-g", strconv.Itoa(fps*10), "-error-resilient", "1", "-f", "ivf", "-")
	} else {
		args = append(args, h264Args(h264Encoder(cfg.Encoder), kbps, fps)...)
		args = append(args, "-f", "h264", "-")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+h.g.Display)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if h.codec == "vp8" {
		err = h.pumpIVF(out, fps)
	} else {
		err = h.pumpH264(out, fps)
	}
	// an ffmpeg without x11grab (a static build earlier in PATH) ends at once: say so
	// instead of restarting it silently
	if werr := cmd.Wait(); err == nil && werr != nil && ctx.Err() == nil {
		err = fmt.Errorf("ffmpeg: %v: %s", werr, strings.TrimSpace(stderr.String()))
	}
	return err
}

// pumpH264 cuts the Annex-B stream into access units at the access unit delimiters (aud=1)
// and sends each as one sample.
func (h *hub) pumpH264(r io.Reader, fps int) error {
	br := bufio.NewReaderSize(r, 1<<20)
	aud := []byte{0, 0, 0, 1, 9}
	var au []byte
	last := time.Now()
	buf := make([]byte, 64<<10)
	flush := func(frame []byte) error {
		now := time.Now()
		d := now.Sub(last)
		if d <= 0 || d > time.Second {
			d = time.Second / time.Duration(fps)
		}
		last = now
		return h.video.WriteSample(media.Sample{Data: frame, Duration: d})
	}
	for {
		n, err := br.Read(buf)
		au = append(au, buf[:n]...)
		for len(au) > 0 {
			i := bytes.Index(au[1:], aud) // the next delimiter ends the current unit
			if i < 0 {
				break
			}
			if err := flush(append([]byte(nil), au[:i+1]...)); err != nil {
				return err
			}
			au = au[i+1:]
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (h *hub) pumpIVF(r io.Reader, fps int) error {
	ivf, _, err := ivfreader.NewWith(r)
	if err != nil {
		return err
	}
	last := time.Now()
	for {
		frame, _, err := ivf.ParseNextFrame()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		now := time.Now()
		d := now.Sub(last)
		if d <= 0 || d > time.Second {
			d = time.Second / time.Duration(fps)
		}
		last = now
		if err := h.video.WriteSample(media.Sample{Data: frame, Duration: d}); err != nil {
			return err
		}
	}
}

func (h *hub) runAudio(ctx context.Context) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "pulse", "-fragment_size", "960",
			"-i", h.g.Sink+".monitor", "-ac", "2", "-ar", "48000", "-c:a", "libopus", "-b:a", "96k",
			"-application", "audio", "-frame_duration", "20", "-page_duration", "20000", "-f", "ogg", "-")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			log.Printf("rtc audio: %v", err)
			sleep(ctx, 5*time.Second)
			continue
		}
		if ogg, _, err := oggreader.NewWith(out); err == nil {
			var last uint64
			for {
				page, hdr, err := ogg.ParseNextPage()
				if err != nil {
					break
				}
				if bytes.HasPrefix(page, []byte("OpusHead")) || bytes.HasPrefix(page, []byte("OpusTags")) {
					continue
				}
				samples := hdr.GranulePosition - last
				last = hdr.GranulePosition
				if samples == 0 || samples > 48000 {
					samples = 960 // 20 ms
				}
				if err := h.audio.WriteSample(media.Sample{Data: page, Duration: time.Duration(samples) * time.Second / 48000}); err != nil {
					break
				}
			}
		}
		cmd.Process.Kill()
		cmd.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" && ctx.Err() == nil {
			log.Printf("rtc audio: ffmpeg: %s", msg)
			sleep(ctx, 4*time.Second)
		}
		sleep(ctx, time.Second)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// rtcOffer answers a browser's offer. Query: codec=h264|vp8 (what the browser can decode).
func (g *Game) rtcOffer(w http.ResponseWriter, r *http.Request) {
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&offer); err != nil || offer.Type != webrtc.SDPTypeOffer {
		http.Error(w, "bad offer", 400)
		return
	}
	codec := "h264"
	if r.URL.Query().Get("codec") == "vp8" {
		codec = "vp8"
	}
	a, cfg := api()
	pc, err := a.NewPeerConnection(webrtc.Configuration{ICEServers: cfg.ICEServers})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	h := g.hub(codec)
	for _, t := range []*webrtc.TrackLocalStaticSample{h.video, h.audio} {
		sender, err := pc.AddTrack(t)
		if err != nil {
			pc.Close()
			http.Error(w, err.Error(), 500)
			return
		}
		video := t == h.video
		go func() { // read RTCP: keyframe requests (PLI / FIR) and the viewer's loss and bandwidth
			for {
				pkts, _, err := sender.ReadRTCP()
				if err != nil {
					return
				}
				if !video {
					continue
				}
				for _, p := range pkts {
					switch p := p.(type) {
					case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
						h.keyframe()
					case *rtcp.ReceiverReport:
						for _, rr := range p.Reports {
							h.mu.Lock()
							if h.ctl.loss(float64(rr.FractionLost)/256, time.Now()) {
								h.retarget()
							}
							h.mu.Unlock()
						}
					case *rtcp.ReceiverEstimatedMaximumBitrate:
						h.mu.Lock()
						if h.ctl.estimate(p.Bitrate, time.Now()) {
							h.retarget()
						}
						h.mu.Unlock()
					}
				}
			}
		}()
	}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			var ev Input
			if json.Unmarshal(m.Data, &ev) == nil {
				g.handle(ev)
			}
		})
	})
	var mu sync.Mutex
	joined := false
	leave := func() {
		mu.Lock()
		defer mu.Unlock()
		if joined {
			joined = false
			h.leave()
		}
	}
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		switch s {
		case webrtc.PeerConnectionStateConnected:
			mu.Lock()
			if !joined {
				joined = true
				h.join()
			}
			mu.Unlock()
		case webrtc.PeerConnectionStateDisconnected: // may recover (network switch); give it 15 s
			time.AfterFunc(15*time.Second, func() {
				if pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					pc.Close()
				}
			})
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			pc.Close()
			leave()
		}
	})
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		http.Error(w, err.Error(), 400)
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err == nil {
		gathered := webrtc.GatheringCompletePromise(pc)
		if err = pc.SetLocalDescription(answer); err == nil {
			select {
			case <-gathered:
			case <-time.After(5 * time.Second):
			}
		}
	}
	if err != nil {
		pc.Close()
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pc.LocalDescription())
}

// rtcConfig tells the page which ICE servers to use.
func (g *Game) rtcConfig(w http.ResponseWriter, r *http.Request) {
	_, cfg := api()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"iceServers": cfg.ICEServers})
}

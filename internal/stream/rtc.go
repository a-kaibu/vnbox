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
	"errors"
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

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"

	"golang.org/x/sys/unix"

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
	// resend packets the viewer reports lost (NACK) instead of waiting for its keyframe request,
	// and send sender reports. No transport-cc: the browser keeps sending REMB, which abr.go reads.
	ir := &interceptor.Registry{}
	if err := webrtc.ConfigureNack(m, ir); err != nil {
		return err
	}
	if err := webrtc.ConfigureRTCPReports(ir); err != nil {
		return err
	}
	rtcAPI = webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se), webrtc.WithInterceptorRegistry(ir))
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
	kick     chan struct{} // start a new encoder so the next frame is a keyframe
	lastKick time.Time
	ctl      *rateCtl  // the video rate the viewer's connection allows (abr.go)
	rate     int       // the rate the encoder runs at (kbit/s)
	rateAt   time.Time // when rate last changed
	noGrab   bool      // the change-driven capture is not available on this display (logged once)

	out     sync.Mutex // the video track: which encoder feeds it, and when its last frame went out
	active  *encoder
	lastOut time.Time
}

// rateHold is the least time between two steps up: a browser estimate wavering around a ladder
// step must not swap encoders every second. Steps down apply at once.
const rateHold = 5 * time.Second

// retarget starts an encoder at the new rate when the rate changed (h.mu held): ffmpeg cannot
// change the rate of a running encoder, so a new one takes over (see encodeDamage).
func (h *hub) retarget() {
	r := h.ctl.kbps()
	if r == h.rate || r > h.rate && time.Since(h.rateAt) < rateHold {
		return
	}
	log.Printf("rtc %s: video %d → %d kbit/s", h.g.Display, h.rate, r)
	h.rate = r
	h.rateAt = time.Now()
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
// ffmpeg cannot be told to emit one, so a new encoder takes over; at most every 3 s.
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
	// separate stream ids: the browser then does not hold the picture back to line it up
	// with the audio (lip sync matters little here, a late picture does)
	v, _ := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: mime}, "video", "vnbox-video")
	a, _ := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "vnbox-audio")
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

// runVideo encodes the game window; when the window moves or changes size it starts over
// (the next keyframe carries the new resolution).
func (h *hub) runVideo(ctx context.Context) {
	for ctx.Err() == nil {
		win, ok := h.g.dev().Largest()
		if !ok || win.Area() <= 1 {
			sleep(ctx, time.Second)
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		gr, err := x11.NewGrabber(x11.Display(h.g.Display), win)
		if err != nil {
			h.mu.Lock()
			if !h.noGrab {
				log.Printf("rtc %s: no change-driven capture (%v), capturing every frame with x11grab", h.g.Display, err)
				h.noGrab = true
			}
			h.mu.Unlock()
		}
		go func() { // start over when the window moves / changes size (x11grab: also on a keyframe request)
			var kick chan struct{}
			if gr == nil {
				kick = h.kick
			}
			for wctx.Err() == nil {
				select {
				case <-kick:
					cancel()
					return
				case <-wctx.Done():
					return
				case <-time.After(time.Second):
				}
				if w, ok := h.g.dev().Largest(); ok && (w.W != win.W || w.H != win.H || w.X != win.X || w.Y != win.Y) {
					cancel()
					return
				}
			}
		}()
		if gr != nil {
			err = h.encodeDamage(wctx, gr)
			gr.Close()
		} else {
			err = h.encodeGrab(wctx, win)
		}
		if err != nil && wctx.Err() == nil {
			log.Printf("rtc video %s: %v", h.g.Display, err)
			sleep(ctx, 2*time.Second)
		}
		cancel()
	}
}

// encoder is one ffmpeg process making the video stream.
type encoder struct {
	cmd     *exec.Cmd
	in      io.WriteCloser // raw frames (nil for x11grab)
	pending bool           // not on the track yet: takes over with its first frame (h.out held)
	live    chan struct{}  // closed when it took over the track
	done    chan struct{}  // closed when ffmpeg ended; err says why
	err     error
}

func (e *encoder) write(frame []byte) error {
	if e == nil {
		return nil
	}
	_, err := e.in.Write(frame)
	return err
}

// startEncoder runs ffmpeg with the input options in front of the codec's.
func (h *hub) startEncoder(ctx context.Context, input []string, fps int, stdin bool) (*encoder, error) {
	_, cfg := api()
	kbps := h.currentRate()
	rate := strconv.Itoa(kbps) + "k"
	args := append([]string{"-v", "error"}, input...)
	if h.codec == "vp8" {
		args = append(args, "-vf", "crop=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p",
			"-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-lag-in-frames", "0",
			"-b:v", rate, "-maxrate", rate, "-crf", "10", "-g", strconv.Itoa(fps*10), "-error-resilient", "1", "-f", "ivf")
	} else {
		args = append(args, h264Args(h264Encoder(cfg.Encoder), kbps, fps)...)
		args = append(args, "-f", "h264")
	}
	args = append(args, "-flush_packets", "1", "-") // each frame out at once, not when a buffer fills
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+h.g.Display)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	e := &encoder{cmd: cmd, pending: true, live: make(chan struct{}), done: make(chan struct{})}
	var err error
	if stdin {
		if e.in, err = cmd.StdinPipe(); err != nil {
			return nil, err
		}
		if f, ok := e.in.(*os.File); ok { // a frame is megabytes: fewer, larger pipe writes
			unix.FcntlInt(f.Fd(), unix.F_SETPIPE_SZ, 1<<20)
		}
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		send := func(frame []byte) error { return h.send(e, frame, fps) }
		var err error
		if h.codec == "vp8" {
			err = pumpIVF(out, send)
		} else {
			err = pumpH264(out, send)
		}
		// an ffmpeg without x11grab (a static build earlier in PATH) ends at once: say so
		// instead of restarting it silently
		if werr := cmd.Wait(); err == nil && werr != nil && ctx.Err() == nil {
			err = fmt.Errorf("ffmpeg: %v: %s", werr, strings.TrimSpace(stderr.String()))
		}
		if err == nil && ctx.Err() == nil {
			err = errors.New("ffmpeg ended")
		}
		e.err = err
		close(e.done)
	}()
	return e, nil
}

// retire stops an encoder that no longer feeds (or never fed) the track.
func (h *hub) retire(e *encoder) {
	if e == nil {
		return
	}
	h.out.Lock()
	e.pending = false
	if h.active == e {
		h.active = nil
	}
	h.out.Unlock()
	e.cmd.Process.Kill()
}

// send puts one encoded frame on the video track if e feeds it. A new encoder takes over with
// its first frame (a keyframe); from then on the one it replaces is muted.
func (h *hub) send(e *encoder, frame []byte, fps int) error {
	h.out.Lock()
	defer h.out.Unlock()
	if h.active != e {
		if !e.pending {
			return nil
		}
		h.active, e.pending = e, false
		close(e.live)
	}
	now := time.Now()
	d := now.Sub(h.lastOut)
	if d <= 0 || d > time.Second {
		d = time.Second / time.Duration(fps)
	}
	h.lastOut = now
	return h.video.WriteSample(media.Sample{Data: frame, Duration: d})
}

func (h *hub) fps() int {
	if h.g.FPS < 24 {
		return 30
	}
	return h.g.FPS
}

// encodeDamage feeds ffmpeg only the frames in which something was drawn, at most fps a second:
// a still screen (most of a visual novel) costs neither capture nor encoding. A keyframe request
// or a new rate starts a second encoder on the current picture, and it takes over the track
// with its first frame: no gap while ffmpeg starts.
func (h *hub) encodeDamage(ctx context.Context, gr *x11.Grabber) error {
	fps := h.fps()
	interval := time.Second / time.Duration(fps)
	input := []string{"-f", "rawvideo", "-pixel_format", x11.PixFmt, "-video_size", fmt.Sprintf("%dx%d", gr.W, gr.H),
		"-framerate", strconv.Itoa(fps), "-i", "pipe:0"}
	cur, err := h.startEncoder(ctx, input, fps, true)
	if err != nil {
		return err
	}
	var next *encoder
	defer func() { h.retire(cur); h.retire(next) }()
	var frame []byte
	var grabbed time.Time
	// after a change, the last picture goes in twice more: an encoder that holds a frame back
	// (VAAPI's async queue) then still shows it while the screen stays still
	trail := 0
	tail := time.NewTimer(time.Hour)
	defer tail.Stop()
	put := func() error {
		if err := cur.write(frame); err != nil {
			return err
		}
		next.write(frame)
		return nil
	}
	var nextLive, nextDone chan struct{}
	for {
		if next != nil {
			nextLive, nextDone = next.live, next.done
		} else {
			nextLive, nextDone = nil, nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-cur.done:
			return cur.err
		case <-nextDone:
			return next.err
		case <-nextLive:
			h.retire(cur)
			cur, next = next, nil
		case <-h.kick:
			h.retire(next)
			if next, err = h.startEncoder(ctx, input, fps, true); err != nil {
				return err
			}
			if frame != nil {
				next.write(frame)
			}
		case _, ok := <-gr.Changed():
			if !ok {
				return errors.New("X display closed")
			}
			sleep(ctx, interval-time.Since(grabbed))
			if frame, err = gr.Grab(); err != nil {
				return err
			}
			grabbed = time.Now()
			if err := put(); err != nil {
				return err
			}
			trail = 2
			tail.Reset(interval)
		case <-tail.C:
			if trail > 0 && frame != nil {
				trail--
				if err := put(); err != nil {
					return err
				}
				tail.Reset(interval)
			}
		}
	}
}

// encodeGrab is the fallback without XDAMAGE / MIT-SHM: ffmpeg's x11grab captures every frame.
func (h *hub) encodeGrab(ctx context.Context, win x11.Window) error {
	fps := h.fps()
	e, err := h.startEncoder(ctx, []string{"-f", "x11grab", "-draw_mouse", "0", "-framerate", strconv.Itoa(fps),
		"-video_size", fmt.Sprintf("%dx%d", win.W, win.H), "-i", fmt.Sprintf("%s+%d,%d", h.g.Display, win.X, win.Y)}, fps, false)
	if err != nil {
		return err
	}
	defer h.retire(e)
	select {
	case <-ctx.Done():
		return nil
	case <-e.done:
		return e.err
	}
}

// pumpH264 cuts the Annex-B stream into access units at the access unit delimiters (aud=1)
// and sends each as one sample.
func pumpH264(r io.Reader, send func([]byte) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	aud := []byte{0, 0, 0, 1, 9}
	var au []byte
	buf := make([]byte, 64<<10)
	for {
		n, err := br.Read(buf)
		au = append(au, buf[:n]...)
		for len(au) > 0 {
			i := bytes.Index(au[1:], aud) // the next delimiter ends the current unit
			if i < 0 {
				break
			}
			if err := send(append([]byte(nil), au[:i+1]...)); err != nil {
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

func pumpIVF(r io.Reader, send func([]byte) error) error {
	ivf, _, err := ivfreader.NewWith(r)
	if err != nil {
		return err
	}
	for {
		frame, _, err := ivf.ParseNextFrame()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := send(frame); err != nil {
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
							h.ctl.loss(float64(rr.FractionLost)/256, time.Now())
							h.retarget() // also applies a step up that rateHold put off
							h.mu.Unlock()
						}
					case *rtcp.ReceiverEstimatedMaximumBitrate:
						h.mu.Lock()
						h.ctl.estimate(p.Bitrate, time.Now())
						h.retarget()
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

// rtcprobe connects to a vnbox stream over WebRTC like a browser would and records what it
// receives, to check the server without a browser:
//
//	rtcprobe -url 'http://host:8080/g/<name>/' -cookie 'vnbox=<token>' -codec h264 -sec 10 -out /tmp/probe
//
// It writes <out>.h264 (or .ivf) and <out>.ogg, and prints the bytes received.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

func main() {
	base := flag.String("url", "", "stream page URL (…/g/<name>/)")
	cookie := flag.String("cookie", "", "Cookie header (vnbox=<token>)")
	codec := flag.String("codec", "h264", "h264 or vp8")
	sec := flag.Int("sec", 10, "seconds to record")
	out := flag.String("out", "probe", "output file prefix")
	click := flag.String("click", "", "x,y to click over the data channel after 3 s")
	flag.Parse()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Fatal(err)
	}
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	dc, _ := pc.CreateDataChannel("input", nil)
	var vbytes, abytes, frames atomic.Int64
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if t.Kind() == webrtc.RTPCodecTypeAudio {
			w, _ := oggwriter.New(*out+".ogg", 48000, 2)
			defer w.Close()
			for {
				p, _, err := t.ReadRTP()
				if err != nil {
					return
				}
				abytes.Add(int64(len(p.Payload)))
				w.WriteRTP(p)
			}
		}
		if strings.Contains(strings.ToLower(t.Codec().MimeType), "h264") {
			// reassemble frames (Annex-B) and write them as they are
			f, _ := os.Create(*out + ".h264")
			defer f.Close()
			sb := samplebuilder.New(512, &codecs.H264Packet{}, 90000)
			for {
				p, _, err := t.ReadRTP()
				if err != nil {
					return
				}
				vbytes.Add(int64(len(p.Payload)))
				sb.Push(p)
				for s := sb.Pop(); s != nil; s = sb.Pop() {
					frames.Add(1)
					f.Write(s.Data)
				}
			}
		}
		w, _ := ivfwriter.New(*out+".ivf", ivfwriter.WithCodec(t.Codec().MimeType))
		defer w.Close()
		for {
			p, _, err := t.ReadRTP()
			if err != nil {
				return
			}
			vbytes.Add(int64(len(p.Payload)))
			w.WriteRTP(p)
		}
	})
	offer, _ := pc.CreateOffer(nil)
	gathered := webrtc.GatheringCompletePromise(pc)
	pc.SetLocalDescription(offer)
	<-gathered
	body, _ := json.Marshal(pc.LocalDescription())
	req, _ := http.NewRequest("POST", strings.TrimSuffix(*base, "/")+"/rtc?codec="+*codec, bytes.NewReader(body))
	req.Header.Set("Cookie", *cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		log.Fatalf("offer: %v %v", err, resp)
	}
	var ans webrtc.SessionDescription
	json.NewDecoder(resp.Body).Decode(&ans)
	if err := pc.SetRemoteDescription(ans); err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	for pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
		if time.Since(start) > 15*time.Second {
			log.Fatalf("not connected: %s", pc.ConnectionState())
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("connected in %v\n", time.Since(start).Round(time.Millisecond))
	if *click != "" {
		time.AfterFunc(3*time.Second, func() {
			var x, y int
			fmt.Sscanf(*click, "%d,%d", &x, &y)
			b, _ := json.Marshal(map[string]any{"t": "click", "x": x, "y": y, "b": 1})
			fmt.Println("click over data channel:", dc.SendText(string(b)))
		})
	}
	var lastV int64
	for i := 0; i < *sec; i++ {
		time.Sleep(time.Second)
		v := vbytes.Load()
		fmt.Printf("t=%2ds video %6.0f kbps  audio %4.0f kbps  frames %d\n", i+1, float64(v-lastV)*8/1000, float64(abytes.Load())*8/1000/float64(i+1), frames.Load())
		lastV = v
	}
	pc.Close()
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}

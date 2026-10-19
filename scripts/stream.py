#!/usr/bin/env python3
"""ゲームウィンドウ 1 枚だけをブラウザに配信する最小サーバ (PoC)
  stream.py --display :123 [--sink vn_xxx] [--port 8080] [--fps 15]
映像: ffmpeg x11grab でゲームウィンドウの矩形だけを MJPEG 化 -> multipart/x-mixed-replace
音声: ゲーム専用の PulseAudio シンクの monitor を Opus/WebM にして /audio で配信
入力: ブラウザのクリック/キーを POST /input で受け、xdotool でそのウィンドウに送る
"""
import argparse, http.server, json, os, re, socketserver, subprocess, threading, time

PAGE = """<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,user-scalable=no">
<title>vnbox stream</title><style>
html,body{margin:0;background:#000;height:100%;overflow:hidden;touch-action:none}
#v{width:100vw;height:100vh;object-fit:contain;display:block}
#bar{position:fixed;right:8px;bottom:8px;display:flex;gap:6px}
#bar button{font:14px sans-serif;padding:8px 12px;opacity:.6}
</style></head><body>
<img id="v" src="/video"><audio id="a" preload="none"></audio>
<div id="bar"><button data-k="Return">Enter</button><button data-k="Escape">Esc</button>
<button data-k="ctrl">Skip</button><button data-b="3">右</button></div>
<script>
const v=document.getElementById('v');
function rel(e){const r=v.getBoundingClientRect(),W=v.naturalWidth,H=v.naturalHeight;
 const s=Math.min(r.width/W,r.height/H),ox=(r.width-W*s)/2,oy=(r.height-H*s)/2;
 return [Math.round((e.clientX-r.left-ox)/s),Math.round((e.clientY-r.top-oy)/s)];}
function send(o){fetch('/input',{method:'POST',body:JSON.stringify(o)});}
// モバイルブラウザは操作起点でないと音を出せないので、最初のタップで音声ストリームを開始
let audioOn=false;function startAudio(){if(audioOn)return;audioOn=true;const a=document.getElementById('a');a.src='/audio';a.play().catch(()=>{audioOn=false;});}
document.addEventListener('pointerdown',startAudio,{capture:true});
v.addEventListener('pointerup',e=>{const [x,y]=rel(e);send({t:'click',x,y,b:e.button==2?3:1});});
v.addEventListener('contextmenu',e=>e.preventDefault());
v.addEventListener('wheel',e=>{send({t:'click',x:0,y:0,b:e.deltaY<0?4:5,nomove:1});});
document.addEventListener('keydown',e=>{send({t:'key',k:e.key});e.preventDefault();});
document.querySelectorAll('#bar button').forEach(b=>b.onclick=ev=>{ev.stopPropagation();
 if(b.dataset.b)send({t:'click',x:0,y:0,b:+b.dataset.b,nomove:1});else send({t:'key',k:b.dataset.k});});
</script></body></html>"""

KEYMAP = {"Enter": "Return", " ": "space", "ArrowUp": "Up", "ArrowDown": "Down", "ArrowLeft": "Left",
          "ArrowRight": "Right", "Escape": "Escape", "Backspace": "BackSpace", "Control": "ctrl"}

class Game:
    def __init__(self, display, fps, sink=None):
        self.env = dict(os.environ, DISPLAY=display); self.fps = fps; self.sink = sink
    def audio(self):
        if not self.sink: return
        cmd = ["ffmpeg", "-v", "error", "-f", "pulse", "-fragment_size", "1024", "-i", f"{self.sink}.monitor",
               "-ac", "2", "-c:a", "libopus", "-b:a", "96k", "-application", "audio",
               "-f", "webm", "-cluster_time_limit", "100", "-"]
        p = subprocess.Popen(cmd, stdout=subprocess.PIPE)
        try:
            while True:
                chunk = p.stdout.read(4096)
                if not chunk: break
                yield chunk
        finally:
            p.kill()
    def xdo(self, *a):
        return subprocess.run(["xdotool", *a], env=self.env, capture_output=True, text=True).stdout
    def window(self):
        best = None
        for w in self.xdo("search", "--onlyvisible", "--name", ".").split():
            g = dict(re.findall(r"(\w+)=(\d+)", self.xdo("getwindowgeometry", "--shell", w)))
            if not g: continue
            area = int(g["WIDTH"]) * int(g["HEIGHT"])
            if best is None or area > best[0]: best = (area, w, g)
        return best and (best[1], {k: int(v) for k, v in best[2].items()})
    def frames(self):
        win = None
        while not win:
            win = self.window(); time.sleep(0.5)
        _, g = win
        cmd = ["ffmpeg", "-v", "error", "-f", "x11grab", "-draw_mouse", "0", "-framerate", str(self.fps),
               "-video_size", f"{g['WIDTH']}x{g['HEIGHT']}", "-i", f"{self.env['DISPLAY']}+{g['X']},{g['Y']}",
               "-f", "mjpeg", "-q:v", "5", "-"]
        p = subprocess.Popen(cmd, env=self.env, stdout=subprocess.PIPE)
        buf = b""
        try:
            while True:
                chunk = p.stdout.read(65536)
                if not chunk: break
                buf += chunk
                while True:  # JPEG を SOI/EOI で切り出す
                    s = buf.find(b"\xff\xd8"); e = buf.find(b"\xff\xd9", s + 2)
                    if s < 0 or e < 0: break
                    yield buf[s:e + 2]; buf = buf[e + 2:]
        finally:
            p.kill()
    def input(self, ev):
        win = self.window()
        if not win: return
        w, _ = win
        if ev["t"] == "click":
            if not ev.get("nomove"):
                self.xdo("mousemove", "--window", w, str(ev["x"]), str(ev["y"]))
                time.sleep(0.05)
            self.xdo("click", str(ev.get("b", 1)))
        elif ev["t"] == "key":
            # WM が無いとフォーカスが PointerRoot のままなので、毎回ゲームウィンドウに当てる
            self.xdo("windowfocus", w)
            k = KEYMAP.get(ev["k"], ev["k"])
            if len(k) == 1 or k in KEYMAP.values() or re.match(r"^[A-Za-z0-9_]+$", k):
                self.xdo("key", k)

def serve(game, port):
    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a): pass
        def do_GET(self):
            if self.path == "/audio":
                self.send_response(200); self.send_header("Content-Type", "audio/webm")
                self.send_header("Cache-Control", "no-store"); self.end_headers()
                try:
                    for c in game.audio(): self.wfile.write(c); self.wfile.flush()
                except (BrokenPipeError, ConnectionResetError):
                    pass
                return
            if self.path == "/video":
                self.send_response(200)
                self.send_header("Content-Type", "multipart/x-mixed-replace; boundary=f")
                self.send_header("Cache-Control", "no-store"); self.end_headers()
                try:
                    for jpg in game.frames():
                        self.wfile.write(b"--f\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n" % len(jpg))
                        self.wfile.write(jpg + b"\r\n")
                except (BrokenPipeError, ConnectionResetError):
                    pass
            else:
                body = PAGE.encode(); self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
        def do_POST(self):
            n = int(self.headers.get("Content-Length", 0))
            try: game.input(json.loads(self.rfile.read(n)))
            except Exception as e: print("input error:", e)
            self.send_response(204); self.end_headers()
    class S(socketserver.ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True; allow_reuse_address = True
    S(("0.0.0.0", port), H).serve_forever()

if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--display", required=True); ap.add_argument("--port", type=int, default=8080)
    ap.add_argument("--fps", type=int, default=15); ap.add_argument("--sink")
    a = ap.parse_args(); serve(Game(a.display, a.fps, a.sink), a.port)

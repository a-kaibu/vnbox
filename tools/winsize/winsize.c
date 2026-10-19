/* winsize.exe — Wine の中から見た、表示中のトップレベルウィンドウの位置・大きさを出す。
 * run.sh が X 側の大きさと比べ、食い違い (ゲームは全画面のつもりなのに X のウィンドウが小さい等) を見つける。
 * 出力: 1 行 1 ウィンドウ "x y w h client_w client_h style title" */
#include <windows.h>
#include <stdio.h>
static BOOL CALLBACK cb(HWND h, LPARAM l)
{
    char t[256]; RECT w, c;
    if (!IsWindowVisible(h)) return TRUE;
    GetWindowTextA(h, t, sizeof(t)); GetWindowRect(h, &w); GetClientRect(h, &c);
    if (w.right - w.left <= 1 || !strcmp(t, "Default IME")) return TRUE;
    printf("%ld %ld %ld %ld %ld %ld %08lx %s\n", w.left, w.top, w.right - w.left, w.bottom - w.top, c.right, c.bottom,
           (unsigned long)GetWindowLongA(h, GWL_STYLE), t);
    return TRUE;
}
int main(void) { EnumWindows(cb, 0); return 0; }

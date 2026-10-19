/* clickbtn.exe — インストーラやランチャーの「インストール」「次へ」「同意する」「OK」などのボタンを押す。
 * 既定のボタンが無く Enter が効かないダイアログ (BGI の独自メニュー等) を、vnbox add が先へ進めるのに使う。
 * いちばん手前の (Z 順で最初の) 表示中のトップレベルウィンドウのうち、押せるボタンを持つものについて、
 * 下の優先順でラベルが一致する有効なボタンに BM_CLICK を送る。押したら "clicked <ラベル>" を出して 0 で終わる。
 * ラベルの比較では "&" と "(&N)" のようなアクセラレータ、空白、">" "<" "." を無視する。
 * ボタンを自前で描くインストーラ (子ウィンドウが無い) は、ウィンドウクラスと大きさで決め打ちした位置をクリックする。
 * 1 回目はインストール、2 回目以降 (インストール後にメニューへ戻ったとき) は終了ボタン。
 * clickbtn.exe -list: 表示中のトップレベルウィンドウと子ウィンドウ (クラス名・文字列) を出すだけ (調査用)。 */
#include <windows.h>
#include <wchar.h>
#include <stdio.h>
#include <stdlib.h>

static const wchar_t *labels[] = {
    L"インストール", L"インストール開始", L"インストールの開始", L"インストールする", L"Install",
    L"同意する", L"同意します", L"はい、同意します", L"Iagree", L"Iaccept", L"Accept",
    L"次へ", L"Next", L"はい", L"Yes", L"OK", L"完了", L"Finish", L"閉じる", L"Close", NULL
};

static void normalize(const wchar_t *in, wchar_t *out, int n)
{
    int j = 0;
    for (int i = 0; in[i] && j < n - 1; i++) {
        wchar_t c = in[i];
        if (c == L'(' && in[i + 1] == L'&' && in[i + 2] && in[i + 3] == L')') { i += 3; continue; }
        if (c == L'（' && in[i + 1] == L'&' && in[i + 2] && in[i + 3] == L'）') { i += 3; continue; }
        if (c == L'&' || c == L' ' || c == L'　' || c == L'>' || c == L'<' || c == L'.' || c == L'…' || c == L'＞') continue;
        out[j++] = c;
    }
    out[j] = 0;
}

struct find { int best; HWND button; HWND top; };

/* ボタンを自前で描くインストーラ: クラス名, クライアント領域の大きさ, 1 回目に押す位置, 2 回目以降に押す位置 */
static const struct layout { const wchar_t *cls; int w, h, x1, y1, x2, y2; const char *what; } layouts[] = {
    {L"BGI - Main window", 500, 324, 398, 27, 398, 293, "BGI installer menu"},  /* インストール / インストーラの終了 */
};

static void click_at(HWND h, int x, int y)
{
    POINT pt = {x, y};
    ClientToScreen(h, &pt);
    SetForegroundWindow(h);
    SetCursorPos(pt.x, pt.y);
    Sleep(200);
    mouse_event(MOUSEEVENTF_LEFTDOWN, 0, 0, 0, 0);
    Sleep(80);
    mouse_event(MOUSEEVENTF_LEFTUP, 0, 0, 0, 0);
}

static BOOL CALLBACK layout_cb(HWND h, LPARAM l)
{
    wchar_t cls[64], marker[MAX_PATH];
    RECT rc;
    if (!IsWindowVisible(h)) return TRUE;
    GetClassNameW(h, cls, 64);
    GetClientRect(h, &rc);
    for (unsigned i = 0; i < sizeof(layouts) / sizeof(layouts[0]); i++) {
        const struct layout *L = &layouts[i];
        if (wcscmp(cls, L->cls) || abs(rc.right - L->w) > 8 || abs(rc.bottom - L->h) > 8) continue;
        GetTempPathW(MAX_PATH, marker);
        wcscat(marker, L"vnbox-clickbtn-");
        wcscat(marker, L->cls);
        BOOL again = GetFileAttributesW(marker) != INVALID_FILE_ATTRIBUTES;
        click_at(h, again ? L->x2 : L->x1, again ? L->y2 : L->y1);
        CloseHandle(CreateFileW(marker, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, 0, NULL));
        printf("clicked %s (%s)\n", L->what, again ? "second" : "first");
        *(int *)l = 1;
        return FALSE;
    }
    return TRUE;
}

static BOOL CALLBACK child_cb(HWND h, LPARAM l)
{
    struct find *f = (struct find *)l;
    wchar_t cls[64], text[256], norm[256];
    if (!IsWindowVisible(h) || !IsWindowEnabled(h)) return TRUE;
    GetClassNameW(h, cls, 64);
    if (_wcsicmp(cls, L"Button") && _wcsicmp(cls, L"TButton") && _wcsicmp(cls, L"TBitBtn")) return TRUE;
    LONG style = GetWindowLongW(h, GWL_STYLE) & 0x0f;
    if (!_wcsicmp(cls, L"Button") && style != BS_PUSHBUTTON && style != BS_DEFPUSHBUTTON) return TRUE;
    GetWindowTextW(h, text, 256);
    normalize(text, norm, 256);
    for (int i = 0; labels[i] && i < f->best; i++)
        if (!_wcsicmp(norm, labels[i])) { f->best = i; f->button = h; break; }
    return TRUE;
}

static BOOL CALLBACK top_cb(HWND h, LPARAM l)
{
    struct find *f = (struct find *)l;
    if (!IsWindowVisible(h)) return TRUE;
    f->best = 1000; f->button = NULL;
    EnumChildWindows(h, child_cb, l);
    if (f->button) { f->top = h; return FALSE; }
    return TRUE;
}

static BOOL CALLBACK list_child(HWND h, LPARAM l)
{
    wchar_t cls[64], text[256]; char a[64], b[512];
    GetClassNameW(h, cls, 64); GetWindowTextW(h, text, 256);
    WideCharToMultiByte(CP_UTF8, 0, cls, -1, a, sizeof(a), NULL, NULL);
    WideCharToMultiByte(CP_UTF8, 0, text, -1, b, sizeof(b), NULL, NULL);
    printf("  %s [%s] style=%08lx%s%s\n", a, b, (unsigned long)GetWindowLongW(h, GWL_STYLE),
           IsWindowVisible(h) ? "" : " hidden", IsWindowEnabled(h) ? "" : " disabled");
    return TRUE;
}

static BOOL CALLBACK list_top(HWND h, LPARAM l)
{
    if (!IsWindowVisible(h)) return TRUE;
    list_child(h, l);
    EnumChildWindows(h, list_child, l);
    return TRUE;
}

int main(int argc, char **argv)
{
    struct find f = {1000, NULL, NULL};
    if (argc > 1 && !strcmp(argv[1], "-list")) { EnumWindows(list_top, 0); return 0; }
    EnumWindows(top_cb, (LPARAM)&f);
    if (!f.button) {
        int done = 0;
        EnumWindows(layout_cb, (LPARAM)&done);
        return done ? 0 : 1;
    }
    SetForegroundWindow(f.top);
    PostMessageW(f.button, BM_CLICK, 0, 0); /* SendMessage would block while the click opens a modal dialog */
    char buf[256];
    WideCharToMultiByte(CP_UTF8, 0, labels[f.best], -1, buf, sizeof(buf), NULL, NULL);
    printf("clicked %s\n", buf);
    return 0;
}

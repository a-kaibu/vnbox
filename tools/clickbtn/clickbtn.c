/* clickbtn.exe — インストーラやランチャーの「インストール」「次へ」「同意する」「OK」などのボタンを押す。
 * 既定のボタンが無く Enter が効かないダイアログ (BGI の独自メニュー等) を、vnbox add が先へ進めるのに使う。
 * いちばん手前の (Z 順で最初の) 表示中のトップレベルウィンドウのうち、押せるボタンを持つものについて、
 * 下の優先順でラベルが一致する有効なボタンに BM_CLICK を送る。押したら "clicked <ラベル>" を出して 0 で終わる。
 * ラベルの比較では "&" と "(&N)" のようなアクセラレータ、空白、">" "<" "." を無視する。 */
#include <windows.h>
#include <wchar.h>
#include <stdio.h>

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

int main(void)
{
    struct find f = {1000, NULL, NULL};
    EnumWindows(top_cb, (LPARAM)&f);
    if (!f.button) return 1;
    SetForegroundWindow(f.top);
    SendMessageW(f.button, BM_CLICK, 0, 0);
    char buf[256];
    WideCharToMultiByte(CP_UTF8, 0, labels[f.best], -1, buf, sizeof(buf), NULL, NULL);
    printf("clicked %s\n", buf);
    return 0;
}

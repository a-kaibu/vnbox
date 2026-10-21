/* fillauth.exe <ウィンドウ名> <ID> <パスワード> — DRM のログイン画面 (ソフト電池の「ユーザー認証」等) に
 * ID とパスワードを入れて送信する。vnbox のセッションが、その画面が出たときに呼ぶ。
 * ウィンドウの 1 つめの Edit に ID、パスワード欄 (ES_PASSWORD) に パスワードを入れ、「送信」「OK」「ログイン」の
 * ボタンを押す。「保存する」には触らない (prefix に残るとセーブと一緒に S3 へ上がる。毎回 vnbox が入れる)。
 * 入れたら "filled" を出して 0 で終わる。画面が無ければ 1。 */
#include <windows.h>
#include <shellapi.h>
#include <wchar.h>
#include <stdio.h>

struct fill { const wchar_t *id, *pass; HWND id_edit, pass_edit, send; };

static BOOL CALLBACK child_cb(HWND h, LPARAM l)
{
    struct fill *f = (struct fill *)l;
    wchar_t cls[64], text[256];
    LONG style = GetWindowLongW(h, GWL_STYLE);
    GetClassNameW(h, cls, 64);
    GetWindowTextW(h, text, 256);
    if (!_wcsicmp(cls, L"Edit")) {
        if (style & ES_PASSWORD) { if (!f->pass_edit) f->pass_edit = h; }
        else if (!f->id_edit && !(style & ES_READONLY) && !(style & ES_MULTILINE)) f->id_edit = h;
    } else if (!_wcsicmp(cls, L"Button")) {
        LONG type = style & 0x0f;
        if (!f->send && (type == BS_PUSHBUTTON || type == BS_DEFPUSHBUTTON) &&
            (wcsstr(text, L"送信") || wcsstr(text, L"ログイン") || !_wcsicmp(text, L"OK") || wcsstr(text, L"認証")))
            f->send = h;
    }
    return TRUE;
}

int main(void)
{
    int argc;
    wchar_t **argv = CommandLineToArgvW(GetCommandLineW(), &argc);
    struct fill f = {0};
    HWND top;
    if (argc < 4) { fprintf(stderr, "usage: fillauth <window title> <id> <password>\n"); return 2; }
    top = FindWindowW(NULL, argv[1]);
    if (!top || !IsWindowVisible(top)) return 1;
    f.id = argv[2];
    f.pass = argv[3];
    EnumChildWindows(top, child_cb, (LPARAM)&f);
    if (!f.id_edit || !f.pass_edit || !f.send) { fprintf(stderr, "fields not found\n"); return 1; }
    SendMessageW(f.id_edit, WM_SETTEXT, 0, (LPARAM)f.id);
    SendMessageW(f.pass_edit, WM_SETTEXT, 0, (LPARAM)f.pass);
    /* PostMessage: the click starts the online check, which can take a while */
    PostMessageW(f.send, BM_CLICK, 0, 0);
    printf("filled\n");
    return 0;
}

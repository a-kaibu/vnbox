/* runtime.c — 何もしない WinRT オブジェクトの実体 (xamlstub32.dll)
 *
 * ゲームが XAML Islands (DesktopWindowXamlSource) で入力欄などを作ろうとしたときに、例外で落ちずに先へ進めるための代用品。
 * 画面には何も出さない。各メソッドは gen.py が作った thunk から stub_dispatch に来て、出力引数に
 * ゼロ / NULL / 新しい空オブジェクトを書いて S_OK を返す。
 */
#include <stdio.h>
#include <string.h>
#include "runtime.h"

typedef void *HSTRING_;
HRESULT WINAPI WindowsCreateString(const WCHAR *, UINT32, HSTRING_ *);

static const GUID IID_IUnknown_ = {0x00000000, 0x0000, 0x0000, {0xc0,0,0,0,0,0,0,0x46}};
static const GUID IID_IInspectable_ = {0xaf86e2e0, 0xb12d, 0x4c6a, {0x9c,0x5a,0xd7,0xaa,0x65,0x10,0x1e,0x90}};
static const GUID IID_IAgileObject_ = {0x94ea2b94, 0xe9cc, 0x49e0, {0xc0,0xff,0xee,0x64,0xca,0x8f,0x5b,0x90}};
static const GUID IID_IActivationFactory_ = {0x00000035, 0x0000, 0x0000, {0xc0,0,0,0,0,0,0,0x46}};
static const GUID IID_IDesktopWindowXamlSourceNative = {0x3cbcf1bf, 0x2f76, 0x4e9c, {0x96,0xab,0xe8,0x4b,0x37,0x97,0x25,0x54}};
static const GUID IID_IDesktopWindowXamlSourceNative2 = {0xe3dcd8c7, 0x3057, 0x4692, {0x99,0xc3,0x7b,0x77,0x20,0xaf,0xda,0x31}};

struct obj;
struct tear { const void **vtbl; struct obj *o; };
struct obj {
    LONG ref;
    const struct stub_class *cls;
    HWND hwnd;                 /* DesktopWindowXamlSource: the (empty, zero size) child window */
    struct tear act;           /* IActivationFactory (factories) */
    struct tear native;        /* IDesktopWindowXamlSourceNative(2) */
    struct tear tears[];
};

static void log_once(const char *what)
{
    static char seen[4096][1];
    unsigned h = 0;
    for (const char *p = what; *p; p++) h = h * 31 + (unsigned char)*p;
    if (seen[h % 4096][0]) return;
    seen[h % 4096][0] = 1;
    fprintf(stderr, "xamlstub: %s\n", what);
}

static const void *act_vtbl[];
static const void *native_vtbl[];

static struct obj *new_obj(int cls)
{
    const struct stub_class *c = &stub_classes[cls];
    struct obj *o = calloc(1, sizeof(*o) + c->nifaces * sizeof(struct tear));
    o->ref = 1;
    o->cls = c;
    for (int i = 0; i < c->nifaces; i++) { o->tears[i].vtbl = c->ifaces[i].vtbl; o->tears[i].o = o; }
    o->act.vtbl = act_vtbl; o->act.o = o;
    o->native.vtbl = native_vtbl; o->native.o = o;
    return o;
}

static BOOL is_xaml_source(const struct obj *o)
{
    return !wcscmp(o->cls->name, L"Windows.UI.Xaml.Hosting.DesktopWindowXamlSource");
}

HRESULT WINAPI stub_QueryInterface(void *this, const GUID *iid, void **out)
{
    struct obj *o = ((struct tear *)this)->o;
    void *r = NULL;
    if (IsEqualGUID(iid, &IID_IUnknown_) || IsEqualGUID(iid, &IID_IInspectable_) || IsEqualGUID(iid, &IID_IAgileObject_))
        r = o->cls->nifaces ? (void *)&o->tears[o->cls->default_iface] : (void *)&o->act;
    else if (o->cls->kind == KIND_FACTORY && IsEqualGUID(iid, &IID_IActivationFactory_))
        r = &o->act;
    else if (is_xaml_source(o) && (IsEqualGUID(iid, &IID_IDesktopWindowXamlSourceNative) || IsEqualGUID(iid, &IID_IDesktopWindowXamlSourceNative2)))
        r = &o->native;
    else
        for (int i = 0; i < o->cls->nifaces; i++)
            if (IsEqualGUID(iid, o->cls->ifaces[i].iid)) { r = &o->tears[i]; break; }
    *out = r;
    if (!r) return E_NOINTERFACE;
    InterlockedIncrement(&o->ref);
    return S_OK;
}

ULONG WINAPI stub_AddRef(void *this) { return InterlockedIncrement(&((struct tear *)this)->o->ref); }

ULONG WINAPI stub_Release(void *this)
{
    struct obj *o = ((struct tear *)this)->o;
    ULONG r = InterlockedDecrement(&o->ref);
    if (!r) { if (o->hwnd) DestroyWindow(o->hwnd); free(o); }
    return r;
}

HRESULT WINAPI stub_GetIids(void *this, ULONG *n, GUID **iids) { *n = 0; *iids = NULL; return S_OK; }
HRESULT WINAPI stub_GetRuntimeClassName(void *this, void **name)
{
    const WCHAR *n = ((struct tear *)this)->o->cls->name;
    return WindowsCreateString(n, wcslen(n), name);
}
HRESULT WINAPI stub_GetTrustLevel(void *this, int *level) { *level = 0; return S_OK; }

/* ---- every generated method lands here ---- */
unsigned long long __cdecl stub_dispatch(int id, DWORD *stack)
{
    const struct stub_method *m = &stub_methods[id];
    log_once(m->name);
    for (int i = 0; i < m->nact; i++) {
        const struct stub_action *a = &m->act[i];
        void *p = (void *)stack[1 + a->word];
        if (!p) continue;
        switch (a->kind) {
        case ACT_ZERO: memset(p, 0, a->size); break;
        case ACT_NULL: *(void **)p = NULL; break;
        case ACT_OBJ: {
            struct obj *o = new_obj(a->cls);
            if (FAILED(stub_QueryInterface(&o->tears[0], a->iid, (void **)p))) *(void **)p = NULL;
            stub_Release(&o->tears[0]);
            break; }
        }
    }
    return ((unsigned long long)m->stack_bytes << 32) | S_OK;
}

/* thunk: [esp]=method id, [esp+4]=return address, [esp+8]=this, then the arguments.
 * stdcall: the callee pops this + arguments, whose size differs per method (stub_dispatch returns it in edx). */
__asm__(
    ".globl _stub_thunk_common\n"
    "_stub_thunk_common:\n"
    "    leal 8(%esp), %ecx\n"
    "    pushl %ecx\n"
    "    pushl 4(%esp)\n"
    "    call _stub_dispatch\n"
    "    addl $8, %esp\n"
    "    popl %ecx\n"          /* method id */
    "    popl %ecx\n"          /* return address */
    "    addl %edx, %esp\n"
    "    jmp *%ecx\n");

/* ---- IActivationFactory of factory objects ---- */
static HRESULT WINAPI act_ActivateInstance(void *this, void **out)
{
    struct obj *f = ((struct tear *)this)->o;
    log_once("IActivationFactory.ActivateInstance");
    if (!f->cls->activatable) { *out = NULL; return E_NOTIMPL; }
    struct obj *o = new_obj(f->cls->runtime);
    HRESULT hr = stub_QueryInterface(&o->tears[0], &IID_IInspectable_, out);
    stub_Release(&o->tears[0]);
    return hr;
}
static const void *act_vtbl[] = { STUB_INSPECTABLE_SLOTS, (const void *)act_ActivateInstance };

/* ---- IDesktopWindowXamlSourceNative(2): a real but empty child window that never gets a size ---- */
static LRESULT CALLBACK island_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp)
{
    switch (msg) {
    case WM_WINDOWPOSCHANGING: ((WINDOWPOS *)lp)->cx = ((WINDOWPOS *)lp)->cy = 0; return 0;
    case WM_ERASEBKGND: return 1;
    case WM_PAINT: ValidateRect(hwnd, NULL); return 0;
    }
    return DefWindowProcW(hwnd, msg, wp, lp);
}

static HRESULT WINAPI native_QueryInterface(void *this, const GUID *iid, void **out) { return stub_QueryInterface(this, iid, out); }
static HRESULT WINAPI native_AttachToWindow(void *this, HWND parent)
{
    struct obj *o = ((struct tear *)this)->o;
    static ATOM atom;
    log_once("IDesktopWindowXamlSourceNative.AttachToWindow");
    if (!atom) {
        WNDCLASSW wc = {0};
        wc.lpfnWndProc = island_proc; wc.hInstance = GetModuleHandleW(NULL); wc.lpszClassName = L"vnbox_xaml_island";
        atom = RegisterClassW(&wc);
    }
    if (!o->hwnd) o->hwnd = CreateWindowExW(0, L"vnbox_xaml_island", L"", WS_CHILD, 0, 0, 0, 0, parent, NULL, GetModuleHandleW(NULL), NULL);
    return S_OK;
}
static HRESULT WINAPI native_get_WindowHandle(void *this, HWND *h) { *h = ((struct tear *)this)->o->hwnd; return S_OK; }
static HRESULT WINAPI native_PreTranslateMessage(void *this, const MSG *m, BOOL *r) { *r = FALSE; return S_OK; }
static const void *native_vtbl[] = { (const void *)native_QueryInterface, (const void *)stub_AddRef, (const void *)stub_Release,
    (const void *)native_AttachToWindow, (const void *)native_get_WindowHandle, (const void *)native_PreTranslateMessage };

/* ---- exports ---- */
HRESULT WINAPI DllGetActivationFactory(HSTRING_ classid, void **factory);
const WCHAR *WINAPI WindowsGetStringRawBuffer(HSTRING_, UINT32 *);
__declspec(dllexport) HRESULT WINAPI DllGetActivationFactory(HSTRING_ classid, void **factory)
{
    const WCHAR *name = WindowsGetStringRawBuffer(classid, NULL);
    for (int i = 0; i < stub_nclasses; i++) {
        const struct stub_class *c = &stub_classes[i];
        if (c->kind == KIND_FACTORY && !wcscmp(stub_classes[c->runtime].name, name)) {
            struct obj *f = new_obj(i);
            char buf[256];
            snprintf(buf, sizeof(buf), "factory %ls", name);
            log_once(buf);
            *factory = &f->act;
            return S_OK;
        }
    }
    *factory = NULL;
    return CLASS_E_CLASSNOTAVAILABLE;
}
__declspec(dllexport) HRESULT WINAPI DllCanUnloadNow(void) { return S_FALSE; }

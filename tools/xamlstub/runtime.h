/* runtime.h — xamlstub の共通部 (生成コード gen.py の出力と runtime.c が使う) */
#ifndef XAMLSTUB_RUNTIME_H
#define XAMLSTUB_RUNTIME_H
#define COBJMACROS
#include <windows.h>

enum { ACT_ZERO, ACT_NULL, ACT_OBJ };
enum { KIND_CLASS, KIND_IFACE, KIND_FACTORY };

struct stub_action { int word, kind, size, cls; const GUID *iid; };  /* write through stack word `word` */
struct stub_method { const char *name; int stack_bytes; int nact; struct stub_action act[4]; };
struct stub_iface { const GUID *iid; const void **vtbl; };
struct stub_class {
    const WCHAR *name; int kind; int nifaces; const struct stub_iface *ifaces;
    int default_iface;
    int runtime;      /* KIND_FACTORY: index of the runtime class it creates */
    int activatable;  /* KIND_FACTORY: IActivationFactory::ActivateInstance creates one */
};

extern const struct stub_method stub_methods[];
extern const struct stub_class stub_classes[];
extern const int stub_nclasses;

/* IInspectable の 6 スロット (QueryInterface, AddRef, Release, GetIids, GetRuntimeClassName, GetTrustLevel) */
HRESULT WINAPI stub_QueryInterface(void *, const GUID *, void **);
ULONG WINAPI stub_AddRef(void *);
ULONG WINAPI stub_Release(void *);
HRESULT WINAPI stub_GetIids(void *, ULONG *, GUID **);
HRESULT WINAPI stub_GetRuntimeClassName(void *, void **);
HRESULT WINAPI stub_GetTrustLevel(void *, int *);
#define STUB_INSPECTABLE_SLOTS (const void *)stub_QueryInterface, (const void *)stub_AddRef, (const void *)stub_Release, \
    (const void *)stub_GetIids, (const void *)stub_GetRuntimeClassName, (const void *)stub_GetTrustLevel
#endif

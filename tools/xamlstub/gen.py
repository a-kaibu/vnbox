#!/usr/bin/env python3
"""gen.py <winmd フォルダ> <出力 .c> [クラス名...]

XAML Islands (Windows.UI.Xaml.Hosting.DesktopWindowXamlSource 等) を使うゲームのための、
「何もしない」WinRT 実装 (xamlstub32.dll) の C ソースを .winmd から生成する。

- 指定したランタイムクラスと、そのメソッドが返すクラス/インターフェイスを (深さ制限つきで) たどって、
  各インターフェイスの vtable を x86 stdcall の正確な引数バイト数で作る (ずれるとスタックが壊れる)。
- メソッドは何もしない: 出力引数はゼロ / 空文字列 / 新しい空オブジェクト、イベント登録は無視。
- 実体は runtime.c (QueryInterface・参照カウント・ファクトリ・HWND まわりのネイティブ相互運用)。
"""
import glob, sys
from winmd import Metadata, Type

WINMD, OUT = sys.argv[1], sys.argv[2]
import os
START = sys.argv[3:] or [l.strip() for l in open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'classes.txt')) if l.strip()]
DEPTH = 3
ALLOWED = ('Windows.UI.Xaml', 'Windows.Foundation', 'Windows.UI')

md = Metadata(sorted(glob.glob(WINMD + '/*.winmd')))


def key(ty):
    return repr(ty)


class Cls:
    """a set of interfaces an object answers to"""
    def __init__(self, name, ifaces, default, kind='class'):
        self.name, self.ifaces, self.default, self.kind = name, ifaces, default, kind
        self.index = None


classes, class_by_name, iface_types = [], {}, {}  # iface_types: key -> Type
queue = []


def iface_closure(ty, acc):
    """interface type + everything it requires (substituting generic arguments)"""
    k = key(ty)
    if k in acc: return
    td = md.types.get(ty.name)
    if td is None or td.guid is None: return
    acc[k] = ty
    for req in td.ifaces:
        acc_t = req.subst(ty.args) if ty.kind == 'generic' else req
        if acc_t.kind == 'class' and md.types.get(acc_t.name) and md.types[acc_t.name].is_interface or acc_t.kind == 'generic':
            iface_closure(acc_t, acc)


def runtime_class(name, depth):
    if name in class_by_name: return class_by_name[name]
    td = md.types.get(name)
    if td is None or td.is_interface or td.is_struct or td.is_enum or td.is_delegate: return None
    if not name.startswith(ALLOWED) or depth > DEPTH: return None
    acc = {}
    t = td
    while t is not None:  # the class and its base classes
        for it in t.ifaces:
            iface_closure(it, acc)
        t = md.types.get(t.base) if t.base else None
    default = td.default or (td.ifaces[0] if td.ifaces else None)
    c = Cls(name, list(acc.values()), default)
    c.td = td
    class_by_name[name] = c; classes.append(c)
    queue.append((c, depth))
    return c


def iface_class(ty, depth):
    name = 'iface:' + key(ty)
    if name in class_by_name: return class_by_name[name]
    if depth > DEPTH or not ty.name.startswith(ALLOWED): return None
    acc = {}
    iface_closure(ty, acc)
    if not acc: return None
    c = Cls(name, list(acc.values()), ty, 'iface')
    class_by_name[name] = c; classes.append(c)
    queue.append((c, depth))
    return c


def factory_class(rc):
    td = rc.td
    acc = {}
    for kind, fac in td.factories:
        if fac:
            iface_closure(Type('class', fac), acc)
    c = Cls('factory:' + rc.name, list(acc.values()), None, 'factory')
    c.runtime = rc
    c.activatable = any(k == 'activatable' and f is None for k, f in td.factories)
    class_by_name[c.name] = c; classes.append(c)
    queue.append((c, 0))
    return c


# ---- what each method does with its out parameters ----
def out_action(ty, depth):
    """what to write through a pointer to a value of type ty"""
    if ty.kind == 'prim':
        if ty.name in ('string', 'object'): return ('null',)
        return ('zero', md.size_align(ty)[0])
    if ty.kind == 'value':
        return ('zero', md.size_align(ty)[0])
    if ty.kind == 'szarray':
        return ('null',)
    if ty.kind == 'generic':
        c = iface_class(ty, depth + 1)
        return ('obj', c, ty) if c else ('null',)
    td = md.types.get(ty.name)
    if td is None or td.is_delegate: return ('null',)
    if td.is_interface:
        c = iface_class(ty, depth + 1)
        return ('obj', c, ty) if c else ('null',)
    c = runtime_class(ty.name, depth + 1)
    if c and c.default is not None:
        return ('obj', c, c.default)
    return ('null',)


def words(n):
    return (n + 3) // 4


methods = []  # (iface key, name, argbytes, actions [(word, action)])
vtables = {}  # iface key -> [method index]


def build_vtable(ty, depth):
    k = key(ty)
    if k in vtables: return
    td = md.types[ty.name]
    vtables[k] = []
    for m in td.methods:
        ret = m.ret.subst(ty.args) if ty.kind == 'generic' else m.ret
        w, acts = 0, []
        for pname, pt, is_out in m.params:
            pt = pt.subst(ty.args) if ty.kind == 'generic' else pt
            if pt.kind == 'byref':
                el = pt.elem
                if el.kind == 'szarray':  # ReceiveArray: UINT32 *count, T **data
                    acts.append((w, ('zero', 4))); acts.append((w + 1, ('null',))); w += 2
                else:
                    acts.append((w, out_action(el, depth))); w += 1
            elif pt.kind == 'szarray':  # PassArray / FillArray: UINT32 count, T *data
                w += 2
            else:
                w += words(md.size_align(pt)[0])
        if not (ret.kind == 'prim' and ret.name == 'void'):
            if ret.kind == 'szarray':
                acts.append((w, ('zero', 4))); acts.append((w + 1, ('null',))); w += 2
            else:
                acts.append((w, out_action(ret, depth))); w += 1
        vtables[k].append(len(methods))
        methods.append((k, m.name, w * 4, acts))


for n in START:
    rc = runtime_class(n, 0)
    if rc is None:
        sys.exit(f"unknown class {n}")
    factory_class(rc)
while queue:
    c, depth = queue.pop(0)
    for ty in c.ifaces:
        build_vtable(ty, depth)
for i, c in enumerate(classes):
    c.index = i

# ---- emit C ----
def cname(k):
    return ''.join(ch if ch.isalnum() else '_' for ch in k)


def guid_c(g):
    b = g.bytes
    d4 = ', '.join(f'0x{x:02x}' for x in b[8:])
    return f'{{0x{b[0:4].hex()}, 0x{b[4:6].hex()}, 0x{b[6:8].hex()}, {{{d4}}}}}'


iface_iid = {k: md.iid(ty) for k, ty in ((key(t), t) for c in classes for t in c.ifaces)}
o = open(OUT, 'w', encoding='utf-8')
w = o.write
w('/* generated by tools/xamlstub/gen.py from the Windows SDK metadata — do not edit */\n#include "runtime.h"\n\n')
for k, g in iface_iid.items():
    w(f'static const GUID iid_{cname(k)} = {guid_c(g)}; /* {k} */\n')
w('\n')
for i in range(len(methods)):
    w(f'__attribute__((naked)) static void t{i}(void) {{ __asm__("pushl ${i}\\n\\tjmp _stub_thunk_common"); }}\n')
w('\n')
for k, idxs in vtables.items():
    w(f'static const void *vt_{cname(k)}[] = {{ STUB_INSPECTABLE_SLOTS')
    for j in idxs:
        w(f', (const void *)t{j}')
    w(' };\n')
w('\n')
w('const struct stub_method stub_methods[] = {\n')
for i, (k, name, nbytes, acts) in enumerate(methods):
    a = []
    for word, act in acts:
        if act[0] == 'zero': a.append(f'{{{word}, ACT_ZERO, {act[1]}, -1, NULL}}')
        elif act[0] == 'null': a.append(f'{{{word}, ACT_NULL, 0, -1, NULL}}')
        else: a.append(f'{{{word}, ACT_OBJ, 0, {act[1].index}, &iid_{cname(key(act[2]))}}}')
    w(f'  {{"{k}.{name}", {nbytes + 4}, {len(acts)}, {{{", ".join(a) if a else "{0}"}}}}},\n')
w('};\n\n')
for c in classes:
    w(f'static const struct stub_iface ifs_{c.index}[] = {{')
    w(', '.join(f'{{&iid_{cname(key(t))}, vt_{cname(key(t))}}}' for t in c.ifaces) or '{0}')
    w('};\n')
w('\nconst struct stub_class stub_classes[] = {\n')
for c in classes:
    kind = {'class': 'KIND_CLASS', 'iface': 'KIND_IFACE', 'factory': 'KIND_FACTORY'}[c.kind]
    rt = c.runtime.index if c.kind == 'factory' else -1
    act = 1 if c.kind == 'factory' and c.activatable else 0
    dflt = [key(t) for t in c.ifaces].index(key(c.default)) if c.default is not None and key(c.default) in [key(t) for t in c.ifaces] else 0
    nm = c.name.split(':', 1)[1] if c.kind != 'class' else c.name
    w(f'  {{L"{nm}", {kind}, {len(c.ifaces)}, ifs_{c.index}, {dflt}, {rt}, {act}}},\n')
w('};\nconst int stub_nclasses = %d;\n' % len(classes))
o.close()
print(f'classes {len(classes)}, interfaces {len(vtables)}, methods {len(methods)}')

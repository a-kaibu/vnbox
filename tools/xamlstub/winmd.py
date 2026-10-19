"""winmd.py  .winmd (ECMA-335 メタデータ) から WinRT の型・メソッド・GUID・ファクトリを読む最小限の読み取り器 (dnfile を使用)"""
import hashlib, struct, uuid
import dnfile


def S(x):
    return '' if x is None else str(x)

PRIM = {0x02: 'b1', 0x03: 'c2', 0x04: 'i1', 0x05: 'u1', 0x06: 'i2', 0x07: 'u2', 0x08: 'i4', 0x09: 'u4',
        0x0a: 'i8', 0x0b: 'u8', 0x0c: 'f4', 0x0d: 'f8', 0x0e: 'string', 0x1c: 'object'}
PRIM_SIZE = {'b1': 1, 'c2': 2, 'i1': 1, 'u1': 1, 'i2': 2, 'u2': 2, 'i4': 4, 'u4': 4, 'i8': 8, 'u8': 8,
             'f4': 4, 'f8': 8, 'string': 4, 'object': 4}


class Reader:
    def __init__(self, data):
        self.d, self.p = data, 0

    def u8(self):
        v = self.d[self.p]; self.p += 1; return v

    def cint(self):  # ECMA-335 compressed unsigned integer
        b = self.u8()
        if b & 0x80 == 0: return b
        if b & 0xc0 == 0x80: return ((b & 0x3f) << 8) | self.u8()
        return ((b & 0x1f) << 24) | (self.u8() << 16) | (self.u8() << 8) | self.u8()


class Type:
    """kind: prim / class / value / generic / var / szarray / byref"""
    def __init__(self, kind, name=None, args=None, elem=None, index=None):
        self.kind, self.name, self.args, self.elem, self.index = kind, name, args or [], elem, index

    def __repr__(self):
        if self.kind == 'prim': return self.name
        if self.kind == 'generic': return f"{self.name}<{', '.join(map(repr, self.args))}>"
        if self.kind == 'var': return f"!{self.index}"
        if self.kind in ('szarray', 'byref'): return f"{self.kind}({self.elem!r})"
        return self.name

    def subst(self, args):
        if self.kind == 'var': return args[self.index]
        if self.kind == 'generic': return Type('generic', self.name, [a.subst(args) for a in self.args])
        if self.kind in ('szarray', 'byref'): return Type(self.kind, elem=self.elem.subst(args))
        return self


class Method:
    def __init__(self, name, ret, params):
        self.name, self.ret, self.params = name, ret, params  # params: [(name, Type, is_out)]


class TypeDef:
    def __init__(self, ns, name):
        self.ns, self.name = ns, name
        self.full = f"{ns}.{name}" if ns else name
        self.is_interface = False; self.is_delegate = False; self.is_enum = False; self.is_struct = False
        self.guid = None; self.methods = []; self.ifaces = []  # Type (class or generic)
        self.default = None; self.base = None; self.fields = []  # (name, Type) for structs, underlying for enums
        self.factories = []  # (kind, Type or None): activatable / static / composable
        self.generic_params = 0


class Metadata:
    def __init__(self, paths):
        self.types = {}
        for p in paths:
            self._load(p)

    def _load(self, path):
        pe = dnfile.dnPE(path)
        md = pe.net.mdtables
        self.md = md
        tds = list(md.TypeDef or [])
        blob = lambda b: bytes(b.value) if b is not None else b''

        def coded_name(ci):
            row = ci.row
            if ci.table.name == 'TypeDef' or ci.table.name == 'TypeRef':
                return f"{S(row.TypeNamespace)}.{S(row.TypeName)}" if S(row.TypeNamespace) else S(row.TypeName)
            return None

        def typeref(code):  # TypeDefOrRefOrSpec encoded in a signature
            tag, idx = code & 3, code >> 2
            tbl = [md.TypeDef, md.TypeRef, md.TypeSpec][tag]
            row = tbl[idx - 1]
            if tag == 2:
                return parse_type(Reader(blob(row.Signature)))
            n = f"{S(row.TypeNamespace)}.{S(row.TypeName)}" if S(row.TypeNamespace) else S(row.TypeName)
            return n

        def parse_type(r):
            t = r.u8()
            while t in (0x1f, 0x20):  # custom modifiers
                r.cint(); t = r.u8()
            if t in PRIM: return Type('prim', PRIM[t])
            if t == 0x11: return Type('value', typeref(r.cint()))
            if t == 0x12: return Type('class', typeref(r.cint()))
            if t == 0x15:
                k = r.u8(); base = typeref(r.cint()); n = r.cint()
                return Type('generic', base, [parse_type(r) for _ in range(n)])
            if t == 0x13: return Type('var', index=r.cint())
            if t == 0x1d: return Type('szarray', elem=parse_type(r))
            if t == 0x10: return Type('byref', elem=parse_type(r))
            if t == 0x01: return Type('prim', 'void')
            if t == 0x18: return Type('prim', 'i4')  # native int (not used by WinRT)
            if t == 0x11: pass
            raise ValueError(f"type 0x{t:x}")

        self._parse_type = parse_type
        for td in tds:
            ns, nm = S(td.TypeNamespace), S(td.TypeName)
            if nm == '<Module>': continue
            t = TypeDef(ns, nm)
            t.is_interface = bool(td.Flags.tdInterface)
            ext = td.Extends
            extn = coded_name(ext) if ext and ext.row_index else None
            t.is_enum = extn == 'System.Enum'
            t.is_struct = extn == 'System.ValueType'
            t.is_delegate = extn == 'System.MulticastDelegate'
            if extn and extn not in ('System.Object', 'System.Enum', 'System.ValueType', 'System.MulticastDelegate'):
                t.base = extn
            if '`' in nm: t.generic_params = int(nm.split('`')[1])
            for m in td.MethodList:
                mr = m.row
                r = Reader(blob(mr.Signature))
                cc = r.u8()
                if cc & 0x10: r.cint()
                n = r.cint(); ret = parse_type(r)
                ps = [parse_type(r) for _ in range(n)]
                names = {p.row.Sequence: (S(p.row.Name), bool(p.row.Flags.pdOut)) for p in mr.ParamList}
                params = [(names.get(i + 1, (f"p{i}", False))[0], ps[i], names.get(i + 1, ('', False))[1]) for i in range(n)]
                if S(mr.Name) in ('.ctor',) and not t.is_delegate: continue
                t.methods.append(Method(S(mr.Name), ret, params))
            for f in td.FieldList:
                fr = f.row
                r = Reader(blob(fr.Signature)); r.u8()
                ft = parse_type(r)
                if t.is_enum and S(fr.Name) == 'value__': t.fields = [('value__', ft)]
                elif t.is_struct and not fr.Flags.fdStatic: t.fields.append((S(fr.Name), ft))
            self.types[t.full] = t
            t._td = td
        # interface impls (+ default attribute), guids, factories
        rows_by_td = {id(x._td): x for x in self.types.values() if hasattr(x, '_td') and x._td in tds}
        index = {i + 1: t for i, td in enumerate(tds) for t in [self.types.get((f"{S(td.TypeNamespace)}.{S(td.TypeName)}" if S(td.TypeNamespace) else S(td.TypeName)))] if t}
        impl_default = set()
        ii_rows = list(md.InterfaceImpl or [])
        for ca in (md.CustomAttribute or []):
            ctor = ca.Type.row
            try:
                cls = ctor.Class.row
                an = S(cls.TypeName)
            except Exception:
                continue
            par = ca.Parent
            val = blob(ca.Value)
            if par.table.name == 'TypeDef':
                t = index.get(par.row_index)
                if not t: continue
                if an == 'GuidAttribute' and len(val) >= 18:
                    a, b, c = struct.unpack_from('<IHH', val, 2); d = val[10:18]
                    t.guid = uuid.UUID(fields=(a, b, c, d[0], d[1], int.from_bytes(d[2:], 'big')))
                elif an in ('ActivatableAttribute', 'StaticAttribute', 'ComposableAttribute'):
                    fac = None
                    if len(val) > 2 and val[2] not in (0x00,) and an != 'ActivatableAttribute' or (an == 'ActivatableAttribute' and len(val) > 8 and self._is_sertype(val)):
                        fac = self._ser_string(val, 2)
                    t.factories.append((an.replace('Attribute', '').lower(), fac))
            elif par.table.name == 'InterfaceImpl' and an == 'DefaultAttribute':
                impl_default.add(par.row_index)
        for i, ii in enumerate(ii_rows):
            t = index.get(ii.Class.row_index)
            if not t: continue
            it = ii.Interface
            if it.table.name == 'TypeSpec':
                ty = parse_type(Reader(blob(it.row.Signature)))
            else:
                ty = Type('class', coded_name(it))
            t.ifaces.append(ty)
            if i + 1 in impl_default: t.default = ty

    @staticmethod
    def _is_sertype(val):
        # Activatable(Type factory, uint version) vs Activatable(uint version): a SerString starts with a length byte
        n = val[2]
        return 0 < n < 0x80 and len(val) >= 3 + n and all(32 <= c < 127 for c in val[3:3 + n])

    @staticmethod
    def _ser_string(val, off):
        n = val[off]
        if n == 0xff: return None
        s = val[off + 1:off + 1 + n].decode('utf-8', 'replace')
        return s.split(',')[0]

    # ---- sizes on x86 ----
    def size_align(self, ty):
        if ty.kind == 'prim':
            s = PRIM_SIZE[ty.name]; return s, s
        if ty.kind == 'value':
            if ty.name == 'System.Guid': return 16, 4
            t = self.types.get(ty.name)
            if t is None: raise KeyError(ty.name)
            if t.is_enum: return self.size_align(t.fields[0][1])
            off, al = 0, 1
            for _, ft in t.fields:
                s, a = self.size_align(ft)
                off = (off + a - 1) // a * a + s; al = max(al, a)
            return (off + al - 1) // al * al, al
        return 4, 4

    # ---- WinRT signatures and parameterized IIDs ----
    def sig(self, ty):
        if ty.kind == 'prim':
            return {'object': 'cinterface(IInspectable)', 'string': 'string'}.get(ty.name, ty.name)
        if ty.kind == 'value':
            if ty.name == 'System.Guid': return 'g16'
            t = self.types[ty.name]
            if t.is_enum: return f"enum({t.full};{self.sig(t.fields[0][1])})"
            return f"struct({t.full};{';'.join(self.sig(f) for _, f in t.fields)})"
        if ty.kind == 'generic':
            g = self.types[ty.name]
            return f"pinterface({{{g.guid}}};{';'.join(self.sig(a) for a in ty.args)})"
        t = self.types[ty.name]
        if t.is_interface: return f"{{{t.guid}}}"
        if t.is_delegate: return f"delegate({{{t.guid}}})"
        return f"rc({t.full};{self.sig(t.default)})"

    def iid(self, ty):
        if ty.kind == 'class':
            return self.types[ty.name].guid
        ns = bytes.fromhex('11f47ad57b7342c0abae878b1e16adee')
        h = bytearray(hashlib.sha1(ns + self.sig(ty).encode()).digest()[:16])
        h[6] = (h[6] & 0x0f) | 0x50
        h[8] = (h[8] & 0x3f) | 0x80
        return uuid.UUID(bytes=bytes(h))

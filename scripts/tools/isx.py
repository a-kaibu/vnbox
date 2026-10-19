#!/usr/bin/env python3
"""InstallShield 12 系 setup.exe (オーバーレイに "InstallShield" ヘッダ) の埋め込みファイルを取り出す: isx.py <setup.exe> <出力先>

ブートストラッパを Wine で動かすと展開が途中で止まる版があるため、Wine を使わずに中身 (.msi 等) を得る。
形式: ヘッダ (署名 14 + 件数 2 + 種別 4 + 予備 8 + 2 + 16 バイト) の後に、
各ファイル = 属性 (名前 260 + flags 4 + 予備 4 + サイズ 4 + 予備 8 + unicode 2 + 予備 30 = 312 バイト) + 本体。
新形式 (ISSetupStream, InstallShield 2010 以降) は属性が 48 バイト + UTF-16 の名前で、鍵は名前の UTF-8、復号後に zlib 展開。
flags&2 なら暗号化: key = 名前 ^ {13 35 86 07} の繰り返し、b' = ~(key ^ ニブル入替(b))。flags&4 なら 1024 バイトごとに鍵位置を戻す。
"""
import os, struct, sys, zlib

def main(exe, out):
    d = open(exe, 'rb').read()
    # ヘッダは PE のセクション末尾 (オーバーレイ) にある。本体中の同じ文字列を拾わないようそこから探す
    pe = struct.unpack_from('<I', d, 0x3c)[0]
    nsec, optsz = struct.unpack_from('<H', d, pe + 6)[0], struct.unpack_from('<H', d, pe + 20)[0]
    overlay = max(sum(struct.unpack_from('<II', d, pe + 24 + optsz + 40 * i + 16)) for i in range(nsec))
    pos = d.find(b'InstallShield\0', overlay)
    stream = False
    if pos < 0 or d.find(b'ISSetupStream\0', overlay) == overlay:
        pos, stream = d.find(b'ISSetupStream\0', overlay), True  # InstallShield 2010 以降の形式
    if pos < 0:
        sys.exit('no InstallShield header')
    n = struct.unpack_from('<H', d, pos + 14)[0]
    pos += 14 + 2 + 4 + 8 + 2 + 16
    os.makedirs(out, exist_ok=True)
    swap = bytes(((b << 4 | b >> 4) & 0xff) for b in range(256))
    for _ in range(n):
        if stream:  # 名前の長さ 4 + flags 4 + 予備 2 + サイズ 4 + 予備 8 + unicode 2 + 時刻 8x3 = 48 バイト、続いて UTF-16 の名前
            nlen, flags, size = struct.unpack_from('<I I 2x I', d, pos)
            name = d[pos + 48:pos + 48 + nlen].decode('utf-16-le')
            raw = name.encode('utf-8')
            pos += 48 + nlen
        else:
            raw = d[pos:pos + 260].split(b'\0')[0]
            flags, _x, size = struct.unpack_from('<III', d, pos + 260)
            pos += 312
            name = raw.decode('cp932', 'replace')
        data = d[pos:pos + size]
        pos += size
        if flags & 2:
            magic = b'\x13\x35\x86\x07'
            key = bytes(c ^ magic[i % 4] for i, c in enumerate(raw))
            block = 1024 if flags & 4 else len(data) or 1
            buf = bytearray(len(data))
            for start in range(0, len(data), block):
                chunk = data[start:start + block]
                # ~(key ^ swap(b)) を 1 バイトずつ計算すると遅いので、多倍長整数でまとめて xor する
                k = (key * (len(chunk) // len(key) + 1))[:len(chunk)]
                sw = chunk.translate(swap)
                x = int.from_bytes(sw, 'little') ^ int.from_bytes(k, 'little')
                buf[start:start + len(chunk)] = (x ^ ((1 << (8 * len(chunk))) - 1)).to_bytes(len(chunk), 'little')
            data = bytes(buf)
        dst = os.path.join(out, os.path.basename(name.replace('\\', '/')))
        if stream and data[:2] == b'\x78\x9c':  # 新形式は復号後さらに zlib 圧縮されている
            data = zlib.decompress(data)
        open(dst, 'wb').write(data)
        print(f'{name}\t{size}\tflags={flags:#x}')

if __name__ == '__main__':
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2])

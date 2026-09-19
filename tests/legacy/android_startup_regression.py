"""Targeted binary regression for the YARUS 0.9.0 Android startup defect.

This is NOT an Android emulator or a full ART verifier. It independently checks
code-item register counts, outgoing call argument counts, and update identity.
Run: python3 tests/android_startup_regression.py OLD.apk FIXED.apk
"""
from __future__ import annotations
import hashlib
from pathlib import Path
import struct
import sys
import zipfile


def u32(data: bytes, offset: int) -> int:
    return struct.unpack_from('<I', data, offset)[0]


def uleb(data: bytes, offset: int) -> tuple[int, int]:
    value = shift = 0
    for _ in range(5):
        byte = data[offset]
        offset += 1
        value |= (byte & 127) << shift
        if byte < 128:
            return value, offset
        shift += 7
    raise ValueError('Invalid ULEB128')


def lp(data: bytes, offset: int = 0) -> tuple[bytes, int]:
    size = u32(data, offset)
    end = offset + 4 + size
    if end > len(data):
        raise ValueError('Truncated length-prefixed field')
    return data[offset + 4:end], end


def signer_cert(apk: Path) -> bytes:
    data = apk.read_bytes()
    eocd = data.rfind(b'PK\x05\x06')
    cd = u32(data, eocd + 16)
    assert data[cd - 16:cd] == b'APK Sig Block 42'
    size = struct.unpack_from('<Q', data, cd - 24)[0]
    start = cd - size - 8
    pair_len = struct.unpack_from('<Q', data, start + 8)[0]
    assert u32(data, start + 16) == 0x7109871a
    signers, _ = lp(data[start + 20:start + 16 + pair_len])
    signer, _ = lp(signers)
    signed, _ = lp(signer)
    _, pos = lp(signed)
    certs, _ = lp(signed, pos)
    certificate, _ = lp(certs)
    return certificate


def methods(dex: bytes) -> list[dict]:
    strings = []
    for i in range(u32(dex, 56)):
        offset = u32(dex, u32(dex, 60) + i * 4)
        _, offset = uleb(dex, offset)
        strings.append(dex[offset:dex.index(0, offset)].decode('utf-8'))
    types = [strings[u32(dex, u32(dex, 68) + i * 4)]
             for i in range(u32(dex, 64))]
    method_ids = []
    for i in range(u32(dex, 88)):
        cl, proto, name = struct.unpack_from('<HHI', dex, u32(dex, 92) + i * 8)
        method_ids.append(types[cl] + '->' + strings[name])
    result = []
    for i in range(u32(dex, 96)):
        p = u32(dex, u32(dex, 100) + i * 32 + 24)
        counts = []
        for _ in range(4):
            value, p = uleb(dex, p)
            counts.append(value)
        for _ in range(counts[0] + counts[1]):
            _, p = uleb(dex, p)
            _, p = uleb(dex, p)
        for count in counts[2:]:
            index = 0
            for _ in range(count):
                delta, p = uleb(dex, p)
                index += delta
                _, p = uleb(dex, p)
                code, p = uleb(dex, p)
                regs, ins, outs, tries, debug, length = struct.unpack_from('<4H2I', dex, code)
                result.append(dict(name=method_ids[index], code=code, regs=regs,
                                   ins=ins, outs=outs, length=length))
    return result


def outgoing_words(dex: bytes, method: dict) -> int:
    words = struct.unpack_from('<' + 'H' * method['length'], dex, method['code'] + 16)
    widths = {0x07: 1, 0x0a: 1, 0x0c: 1, 0x0d: 1, 0x0e: 1, 0x0f: 1,
              0x11: 1, 0x14: 3, 0x1a: 2, 0x22: 2, 0x54: 2, 0x5b: 2,
              0x6e: 3, 0x6f: 3, 0x70: 3, 0x71: 3, 0x72: 3, 0x74: 3,
              0x32: 2, 0x33: 2, 0x38: 2, 0x39: 2, 0x29: 2}
    p = maximum = 0
    while p < len(words):
        word, op = words[p], words[p] & 255
        if op in (0x6e, 0x6f, 0x70, 0x71, 0x72):
            maximum = max(maximum, word >> 12)
        elif op == 0x74:
            maximum = max(maximum, word >> 8)
        if op not in widths:
            raise ValueError(f'Unsupported opcode {op:x}; do not silently pass')
        p += widths[op]
    assert p == len(words)
    return maximum


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit('Usage: android_startup_regression.py OLD.apk FIXED.apk')
    old, new = map(Path, sys.argv[1:])
    with zipfile.ZipFile(old) as a, zipfile.ZipFile(new) as b:
        old_dex, new_dex = a.read('classes.dex'), b.read('classes.dex')
        old_methods, new_methods = methods(old_dex), methods(new_dex)
        invalid = [m for m in old_methods if m['outs'] > 5 and m['outs'] > m['regs']]
        assert len(invalid) == 10, invalid
        print('PASS: original APK reproduces 10 ART outs_size/registers_size constraint violations')
        assert len(new_methods) == len(old_methods) == 15
        for m in new_methods:
            assert m['ins'] <= m['regs']
            assert m['outs'] <= 5 or m['outs'] <= m['regs'], m
            assert m['outs'] == outgoing_words(new_dex, m), m
        print('PASS: repaired APK: all 15 methods satisfy the constraint; outgoing sizes match decoded invokes')
        # Verify this is strictly a header-only bytecode fix, not a UI rewrite.
        normalized = bytearray(new_dex)
        normalized[8:32] = old_dex[8:32]
        for om, nm in zip(old_methods, new_methods):
            assert om['name'] == nm['name'] and om['code'] == nm['code']
            off = om['code'] + 4
            normalized[off:off+2] = old_dex[off:off+2]
        assert bytes(normalized) == old_dex
        print('PASS: DEX changes limited to outgoing-size headers and required checksums; method instructions unchanged')
        assert a.read('assets/index.html') == b.read('assets/index.html')
        print('PASS: bundled interface, IndexedDB keys, storage schema, server protocol byte-for-byte unchanged')
        assert a.read('resources.arsc') == b.read('resources.arsc')
        # Names have the same UTF-8/UTF-16 lengths, so only string bytes/code differ.
        expected_manifest = a.read('AndroidManifest.xml').replace(b'0.9.0 beta', b'0.9.1 beta')
        # Update versionCode is encoded as a typed 32-bit integer.
        old_pattern = b'\x08\x00\x00\x10' + struct.pack('<I', 900)
        new_pattern = b'\x08\x00\x00\x10' + struct.pack('<I', 901)
        assert expected_manifest.count(old_pattern) == 1
        expected_manifest = expected_manifest.replace(old_pattern, new_pattern)
        assert expected_manifest == b.read('AndroidManifest.xml')
        print('PASS: manifest unchanged except versionName 0.9.1 beta and versionCode 901; package and permissions unchanged')
    assert signer_cert(old) == signer_cert(new)
    print('PASS: exact original signing certificate retained: ' + hashlib.sha256(signer_cert(new)).hexdigest())
    print('NOT TESTED: installation, Android runtime, real device startup, WebView or network behavior')


if __name__ == '__main__':
    main()

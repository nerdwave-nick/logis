#!/usr/bin/env python3
"""Cuts the system fonts the shaping tests use beyond tests/fonts down to the
characters of their test runs, with every layout feature kept and hinting
dropped, through the system libharfbuzz-subset (ctypes, no Python packages).
Development time only; the subsets are committed with their licences.

    python3 -I internal/shape/testdata/subset.py

Sources (Arch packages): noto-fonts, noto-fonts-emoji, adwaita-fonts;
all SIL Open Font License 1.1 without Reserved Font Names.
"""
import ctypes, hashlib, json, os, sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "fonts")

hb = ctypes.CDLL("libharfbuzz.so.0")
hbs = ctypes.CDLL("libharfbuzz-subset.so.0")
for f, res, args in [
    (hb.hb_blob_create_from_file, ctypes.c_void_p, [ctypes.c_char_p]),
    (hb.hb_face_create, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_uint]),
    (hb.hb_set_add, None, [ctypes.c_void_p, ctypes.c_uint32]),
    (hb.hb_set_clear, None, [ctypes.c_void_p]),
    (hb.hb_set_invert, None, [ctypes.c_void_p]),
    (hb.hb_face_reference_blob, ctypes.c_void_p, [ctypes.c_void_p]),
    (hb.hb_blob_get_data, ctypes.c_void_p, [ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint)]),
    (hbs.hb_subset_input_create_or_fail, ctypes.c_void_p, []),
    (hbs.hb_subset_input_unicode_set, ctypes.c_void_p, [ctypes.c_void_p]),
    (hbs.hb_subset_input_set, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_int]),
    (hbs.hb_subset_input_set_flags, None, [ctypes.c_void_p, ctypes.c_uint]),
    (hbs.hb_subset_or_fail, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_void_p]),
]:
    f.restype, f.argtypes = res, args

SETS_DROP_TABLE_TAG, SETS_LAYOUT_FEATURE_TAG, SETS_LAYOUT_SCRIPT_TAG = 3, 6, 7
FLAGS_NO_HINTING = 0x1

# Subset name → (source file, extra characters beyond those of its runs,
# tables to drop).
SOURCES = {
    "NotoSans-Full.ttf": ("/usr/share/fonts/noto/NotoSans-Regular.ttf", ""),
    "NotoSans-NoGPOS.ttf": ("/usr/share/fonts/noto/NotoSans-Regular.ttf", "", ("GPOS",)),
    "NotoSansArabic-Full.ttf": ("/usr/share/fonts/noto/NotoSansArabic-Regular.ttf", ""),
    "NotoSansHebrew-Full.ttf": ("/usr/share/fonts/noto/NotoSansHebrew-Regular.ttf", ""),
    "NotoNastaliqUrdu-Subset.ttf": ("/usr/share/fonts/noto/NotoNastaliqUrdu-Regular.ttf", ""),
    "NotoSansSyriac-Subset.ttf": ("/usr/share/fonts/noto/NotoSansSyriac-Regular.ttf", ""),
    "NotoColorEmoji-Subset.ttf": ("/usr/share/fonts/noto/NotoColorEmoji.ttf", ""),
    "AdwaitaSans-Subset.ttf": ("/usr/share/fonts/Adwaita/AdwaitaSans-Regular.ttf", ""),
    "AdwaitaMono-Subset.ttf": ("/usr/share/fonts/Adwaita/AdwaitaMono-Regular.ttf", ""),
}


def subset(src, codepoints, dst, drop=()):
    face = hb.hb_face_create(hb.hb_blob_create_from_file(src.encode()), 0)
    inp = hbs.hb_subset_input_create_or_fail()
    uset = hbs.hb_subset_input_unicode_set(inp)
    for cp in sorted(codepoints):
        hb.hb_set_add(uset, cp)
    for which in (SETS_LAYOUT_FEATURE_TAG, SETS_LAYOUT_SCRIPT_TAG):
        s = hbs.hb_subset_input_set(inp, which)
        hb.hb_set_clear(s)
        hb.hb_set_invert(s)
    dropped = hbs.hb_subset_input_set(inp, SETS_DROP_TABLE_TAG)
    for t in drop:
        hb.hb_set_add(dropped, int.from_bytes(t.encode(), "big"))
    hbs.hb_subset_input_set_flags(inp, FLAGS_NO_HINTING)
    out = hbs.hb_subset_or_fail(face, inp)
    if not out:
        sys.exit("subsetting %s failed" % src)
    blob = hb.hb_face_reference_blob(out)
    n = ctypes.c_uint()
    data = ctypes.string_at(hb.hb_blob_get_data(blob, ctypes.byref(n)), n.value)
    with open(dst, "wb") as f:
        f.write(data)
    return hashlib.sha256(open(src, "rb").read()).hexdigest(), len(data)


def main():
    cases = json.load(open(os.path.join(HERE, "cases.json"), encoding="utf-8"))
    chars = {name: set(map(ord, spec[1])) for name, spec in SOURCES.items()}
    for case in cases:
        if case["font"] in chars:
            chars[case["font"]] |= set(map(ord, case["text"]))
    os.makedirs(OUT, exist_ok=True)
    manifest = {}
    for name, spec in sorted(SOURCES.items()):
        src = spec[0]
        digest, size = subset(src, chars[name], os.path.join(OUT, name), spec[2] if len(spec) > 2 else ())
        manifest[name] = {"source": src, "source_sha256": digest, "bytes": size}
        print("%s: %d bytes from %s" % (name, size, src), file=sys.stderr)
    with open(os.path.join(OUT, "manifest.json"), "w") as f:
        json.dump(manifest, f, indent=1, sort_keys=True)
        f.write("\n")


main()

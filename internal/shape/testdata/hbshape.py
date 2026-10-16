#!/usr/bin/env python3
"""The HarfBuzz oracle: shapes every run of cases.json with the system
libharfbuzz (ctypes, no Python packages) the way cosmic-text's shaping
call does — one run, a direction, script guessed from the text, no
language, no flags — and writes glyph ids, clusters, advances and offsets
in font units to hb.json. Development time only; the result is committed
and internal/shape's tests compare against it. HarfBuzz is never linked
into logis.

    python3 -I internal/shape/testdata/hbshape.py
"""
import ctypes, hashlib, json, locale, os, sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", "..", ".."))

# No locale: HarfBuzz would otherwise pick a language from it, and
# cosmic-text shapes without one.
locale.setlocale(locale.LC_ALL, "C")

hb = ctypes.CDLL("libharfbuzz.so.0")


class Info(ctypes.Structure):
    _fields_ = [("codepoint", ctypes.c_uint32), ("mask", ctypes.c_uint32), ("cluster", ctypes.c_uint32),
                ("v1", ctypes.c_uint32), ("v2", ctypes.c_uint32)]


class Pos(ctypes.Structure):
    _fields_ = [("x_advance", ctypes.c_int32), ("y_advance", ctypes.c_int32), ("x_offset", ctypes.c_int32),
                ("y_offset", ctypes.c_int32), ("v", ctypes.c_uint32)]


class Feature(ctypes.Structure):
    _fields_ = [("tag", ctypes.c_uint32), ("value", ctypes.c_uint32), ("start", ctypes.c_uint),
                ("end", ctypes.c_uint)]


for f, res, args in [
    (hb.hb_blob_create_from_file, ctypes.c_void_p, [ctypes.c_char_p]),
    (hb.hb_face_create, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_uint]),
    (hb.hb_font_create, ctypes.c_void_p, [ctypes.c_void_p]),
    (hb.hb_font_set_ppem, None, [ctypes.c_void_p, ctypes.c_uint, ctypes.c_uint]),
    (hb.hb_buffer_create, ctypes.c_void_p, []),
    (hb.hb_buffer_destroy, None, [ctypes.c_void_p]),
    (hb.hb_buffer_add_utf8, None, [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_int, ctypes.c_uint, ctypes.c_int]),
    (hb.hb_buffer_set_direction, None, [ctypes.c_void_p, ctypes.c_int]),
    (hb.hb_buffer_guess_segment_properties, None, [ctypes.c_void_p]),
    (hb.hb_buffer_get_language, ctypes.c_void_p, [ctypes.c_void_p]),
    (hb.hb_language_to_string, ctypes.c_char_p, [ctypes.c_void_p]),
    (hb.hb_feature_from_string, ctypes.c_int, [ctypes.c_char_p, ctypes.c_int, ctypes.POINTER(Feature)]),
    (hb.hb_shape, None, [ctypes.c_void_p, ctypes.c_void_p, ctypes.POINTER(Feature), ctypes.c_uint]),
    (hb.hb_version_string, ctypes.c_char_p, []),
    (hb.hb_buffer_get_glyph_infos, ctypes.POINTER(Info), [ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint)]),
    (hb.hb_buffer_get_glyph_positions, ctypes.POINTER(Pos), [ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint)]),
]:
    f.restype, f.argtypes = res, args

LTR, RTL = 4, 5


def font_path(name):
    for d in ("tests/fonts", "internal/shape/testdata/fonts"):
        p = os.path.join(ROOT, d, name)
        if os.path.exists(p):
            return p
    sys.exit("no font " + name)


def main():
    cases = json.load(open(os.path.join(HERE, "cases.json"), encoding="utf-8"))
    fonts, digests = {}, {}
    out = {"harfbuzz": hb.hb_version_string().decode(), "fonts": digests, "runs": []}
    for case in cases:
        name, ppem = case["font"], case.get("ppem", 0)
        key = (name, ppem)
        if key not in fonts:
            path = font_path(name)
            digests[name] = hashlib.sha256(open(path, "rb").read()).hexdigest()
            font = hb.hb_font_create(hb.hb_face_create(hb.hb_blob_create_from_file(path.encode()), 0))
            if ppem:
                hb.hb_font_set_ppem(font, ppem, ppem)
            fonts[key] = font
        feats = (Feature * max(1, len(case.get("features", []))))()
        for i, s in enumerate(case.get("features", [])):
            if not hb.hb_feature_from_string(s.encode(), -1, ctypes.byref(feats[i])):
                sys.exit("bad feature " + s)
        buf = hb.hb_buffer_create()
        text = case["text"].encode()
        hb.hb_buffer_add_utf8(buf, text, len(text), 0, len(text))
        hb.hb_buffer_set_direction(buf, RTL if case.get("rtl") else LTR)
        hb.hb_buffer_guess_segment_properties(buf)
        lang = hb.hb_language_to_string(hb.hb_buffer_get_language(buf))
        if lang not in (None, b"c"):
            sys.exit("unexpected language %r" % lang)
        hb.hb_shape(fonts[key], buf, feats, len(case.get("features", [])))
        n = ctypes.c_uint()
        infos = hb.hb_buffer_get_glyph_infos(buf, ctypes.byref(n))
        pos = hb.hb_buffer_get_glyph_positions(buf, ctypes.byref(n))
        glyphs = [[infos[i].codepoint, infos[i].cluster, pos[i].x_advance, pos[i].x_offset, pos[i].y_offset]
                  for i in range(n.value)]
        out["runs"].append(dict(case, glyphs=glyphs))
        hb.hb_buffer_destroy(buf)
    with open(os.path.join(HERE, "hb.json"), "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=0)
        f.write("\n")
    print("%d runs, HarfBuzz %s" % (len(out["runs"]), out["harfbuzz"]), file=sys.stderr)


main()

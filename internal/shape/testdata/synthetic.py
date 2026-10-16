#!/usr/bin/env python3
"""Builds fonts/Synthetic-Layout.ttf: Noto Sans cut to ASCII and two marks
(through the system libharfbuzz-subset), its GDEF/GSUB/GPOS replaced by
hand-made tables with the lookup types and formats the other test fonts
lack — context and chain-context format 1/2, reverse chaining, GSUB
extension, single positioning format 2, mark-to-ligature, contextual
positioning, a mark filtering set, nested lookups that change the
sequence length, and hinting device tables (formats 1–3). HarfBuzz then
shapes it like any other test font (hbshape.py).

    python3 -I internal/shape/testdata/synthetic.py
"""
import ctypes, os, struct

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = "/usr/share/fonts/noto/NotoSans-Regular.ttf"
OUT = os.path.join(HERE, "fonts", "Synthetic-Layout.ttf")

hb = ctypes.CDLL("libharfbuzz.so.0")
hbs = ctypes.CDLL("libharfbuzz-subset.so.0")
for f, res, args in [
    (hb.hb_blob_create_from_file, ctypes.c_void_p, [ctypes.c_char_p]),
    (hb.hb_blob_create, ctypes.c_void_p, [ctypes.c_char_p, ctypes.c_uint, ctypes.c_int, ctypes.c_void_p, ctypes.c_void_p]),
    (hb.hb_face_create, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_uint]),
    (hb.hb_font_create, ctypes.c_void_p, [ctypes.c_void_p]),
    (hb.hb_font_get_nominal_glyph, ctypes.c_int, [ctypes.c_void_p, ctypes.c_uint32, ctypes.POINTER(ctypes.c_uint32)]),
    (hb.hb_set_add, None, [ctypes.c_void_p, ctypes.c_uint32]),
    (hb.hb_face_reference_blob, ctypes.c_void_p, [ctypes.c_void_p]),
    (hb.hb_blob_get_data, ctypes.c_void_p, [ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint)]),
    (hbs.hb_subset_input_create_or_fail, ctypes.c_void_p, []),
    (hbs.hb_subset_input_unicode_set, ctypes.c_void_p, [ctypes.c_void_p]),
    (hbs.hb_subset_input_set, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_int]),
    (hbs.hb_subset_input_set_flags, None, [ctypes.c_void_p, ctypes.c_uint]),
    (hbs.hb_subset_or_fail, ctypes.c_void_p, [ctypes.c_void_p, ctypes.c_void_p]),
]:
    f.restype, f.argtypes = res, args


def tag(s):
    return struct.unpack(">I", s.encode())[0]


def base_font():
    face = hb.hb_face_create(hb.hb_blob_create_from_file(SRC.encode()), 0)
    inp = hbs.hb_subset_input_create_or_fail()
    u = hbs.hb_subset_input_unicode_set(inp)
    for cp in list(range(0x20, 0x7F)) + [0x0301, 0x0323, 0xFB01]:
        hb.hb_set_add(u, cp)
    drop = hbs.hb_subset_input_set(inp, 3)  # HB_SUBSET_SETS_DROP_TABLE_TAG
    for t in ("GSUB", "GPOS", "GDEF"):
        hb.hb_set_add(drop, tag(t))
    hbs.hb_subset_input_set_flags(inp, 0x1)  # no hinting
    out = hbs.hb_subset_or_fail(face, inp)
    blob = hb.hb_face_reference_blob(out)
    n = ctypes.c_uint()
    return ctypes.string_at(hb.hb_blob_get_data(blob, ctypes.byref(n)), n.value)


class T:
    """A table: 16-bit words and offsets to child tables (16- or 32-bit,
    relative to this table's start), serialised depth first."""

    def __init__(self, *items):
        self.items = list(items)

    def bytes(self):
        head, children, patches = b"", [], []
        for it in self.items:
            if isinstance(it, T):
                patches.append((len(head), it, 2))
                head += b"\0\0"
            elif isinstance(it, tuple) and it[0] == "o32":
                patches.append((len(head), it[1], 4))
                head += b"\0\0\0\0"
            elif isinstance(it, tuple) and it[0] == "u24":
                head += struct.pack(">I", it[1])[1:]
            elif it is None:
                head += b"\0\0"
            else:
                head += struct.pack(">H", it & 0xFFFF)
        out = bytearray(head)
        for pos, child, size in patches:
            off = len(out)
            out += child.bytes()
            out[pos:pos + size] = struct.pack(">H" if size == 2 else ">I", off)
        return bytes(out)


def coverage(gids):
    gids = sorted(gids)
    return T(1, len(gids), *gids)


def classdef(mapping):
    items = sorted(mapping.items())
    return T(2, len(items), *[x for g, c in items for x in (g, g, c)])


def device(start, deltas, fmt):
    bits = {1: 2, 2: 4, 3: 8}[fmt]
    per = 16 // bits
    words = []
    for i in range(0, len(deltas), per):
        w = 0
        for j, d in enumerate(deltas[i:i + per]):
            w |= (d & ((1 << bits) - 1)) << (16 - bits * (j + 1))
        words.append(w)
    return T(start, start + len(deltas) - 1, fmt, *words)


def anchor(x, y, xdev=None, ydev=None):
    if xdev or ydev:
        return T(3, x, y, xdev, ydev)
    return T(1, x, y)


def lookup(kind, subtables, flag=0, markset=None):
    items = [kind, flag, len(subtables), *subtables]
    if markset is not None:
        items.append(markset)
    return T(*items)


def layout(lookups, features):
    """features: tag → lookup indices, in every script's default LangSys."""
    tags = sorted(features)
    feature_list = T(len(tags), *[x for t in tags for x in ((t >> 16), t & 0xFFFF, T(0, len(features[t]), *features[t]))])
    langsys = T(0, 0xFFFF, len(tags), *range(len(tags)))
    scripts = T(2, tag("DFLT") >> 16, tag("DFLT") & 0xFFFF, T(langsys, 0),
                tag("latn") >> 16, tag("latn") & 0xFFFF, T(langsys, 0))
    return T(1, 0, scripts, feature_list, T(len(lookups), *lookups)).bytes()


def main():
    font = base_font()
    hbfont = hb.hb_font_create(hb.hb_face_create(hb.hb_blob_create(font, len(font), 1, None, None), 0))

    def g(ch):
        out = ctypes.c_uint32()
        if not hb.hb_font_get_nominal_glyph(hbfont, ord(ch), ctypes.byref(out)):
            raise SystemExit("no glyph for %r" % ch)
        return out.value

    acute, dotbelow, fi = g("́"), g("̣"), g("ﬁ")
    letters = [g(c) for c in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"]

    gdef = T(1, 2, classdef({**{l: 1 for l in letters}, acute: 3, dotbelow: 3, fi: 2}), None, None, None,
             T(1, 1, ("o32", coverage([acute])))).bytes()

    single1 = lambda src, dst: T(1, coverage([g(src)]), g(dst) - g(src))
    single2 = lambda pairs: T(2, coverage([g(s) for s, _ in pairs]), len(pairs),
                              *[g(d) for _, d in sorted(pairs, key=lambda p: g(p[0]))])
    rec = lambda seq, lk: (seq, lk)
    gsub_lookups = [
        # 0: context format 1: "ab" → a becomes A (lookup 9).
        lookup(5, [T(1, coverage([g("a")]), 1, T(1, T(2, 1, g("b"), *rec(0, 9))))]),
        # 1: context format 2: c then class 2 (d) → d becomes D (lookup 10).
        lookup(5, [T(2, coverage([g("c")]), classdef({g("c"): 1, g("d"): 2}), 2, None,
                     T(1, T(2, 1, 2, *rec(1, 10))))]),
        # 2: chain context format 1: x | e | f → e becomes E (lookup 11).
        lookup(6, [T(1, coverage([g("e")]), 1, T(1, T(1, g("x"), 1, 1, g("f"), 1, *rec(0, 11))))]),
        # 3: extension wrapping g → G.
        lookup(7, [T(1, 1, ("o32", single2([("g", "G")])))]),
        # 4: reverse chaining: i | h | j → H.
        lookup(8, [T(1, coverage([g("h")]), 1, coverage([g("i")]), 1, coverage([g("j")]), 1, g("H"))]),
        # 5: ligature f i → ﬁ, marks between allowed.
        lookup(4, [T(1, coverage([g("f")]), 1, T(1, T(fi, 2, g("i"))))]),
        # 6: context format 1 with nested lookups that grow the sequence:
        # "pq" → p becomes p p p (lookup 12), then the glyph at position 1
        # becomes Q (lookup 13).
        lookup(5, [T(1, coverage([g("p")]), 1, T(1, T(2, 2, g("q"), *rec(0, 12), *rec(1, 13))))]),
        None, None,
        # 9–13: nested.
        lookup(1, [single1("a", "A")]),
        lookup(1, [single2([("d", "D")])]),
        lookup(1, [single2([("e", "E")])]),
        lookup(2, [T(1, coverage([g("p")]), 1, T(3, g("p"), g("p"), g("p")))]),
        lookup(1, [single2([("p", "Q"), ("q", "Q")])]),
    ]
    gsub_lookups[7] = lookup(1, [single2([("z", "Z")])])  # unused
    gsub_lookups[8] = lookup(1, [single2([("y", "Y")])])  # unused
    gsub = layout(gsub_lookups, {tag("calt"): [0, 1, 2, 3, 4, 6], tag("liga"): [5]})

    vf = 0x04 | 0x40  # x advance + its device
    pos1 = lambda gl, fmt, values: T(1, coverage([g(gl)]), fmt, *values)
    gpos_lookups = [
        # 0: single format 2 with device tables of each format.
        lookup(1, [T(2, coverage([g("v"), g("w"), g("y")]), vf, 3,
                     -50, device(10, [1, -1, 0, -2], 1),
                     20, device(12, [7, -8, 3], 2),
                     0, device(9, [-3, 100, 0, 5, -128, 127, 1], 3))]),
        # 1: pair format 1, the first glyph's advance with a device table.
        lookup(2, [T(1, coverage([g("T")]), vf, 0x01, 1,
                     T(1, g("o"), -120, device(11, [-1, 1, -2, 2], 1), 15))]),
        # 2: mark to ligature: ﬁ's two components take an acute above and a
        # dot below each.
        lookup(5, [T(1, coverage([acute, dotbelow]), coverage([fi]), 2,
                     T(2, 0, anchor(250, 700), 1, anchor(250, -30)),
                     T(1, T(2, anchor(150, 760), anchor(160, -40, device(12, [2, 3], 2), None),
                            anchor(420, 760, None, device(12, [-4, 4], 3)), anchor(430, -40))))]),
        # 3: mark to base, filtered to the acute (mark set 0): the dot
        # below is skipped.
        lookup(4, [T(1, coverage([acute, dotbelow]), coverage([g(c) for c in "aeo"]), 2,
                     T(2, 0, anchor(250, 700), 1, anchor(250, -30)),
                     T(3, anchor(280, 560), anchor(280, 0), anchor(300, 560), anchor(300, 0),
                       anchor(310, 560), anchor(310, 0)))], flag=0x10, markset=0),
        # 4: context positioning format 1: "mn" → n moves (lookup 8).
        lookup(7, [T(1, coverage([g("m")]), 1, T(1, T(2, 1, g("n"), 1, 8)))]),
        # 5: context positioning format 2: r then class 2 (s) → r wider (9).
        lookup(7, [T(2, coverage([g("r")]), classdef({g("r"): 1, g("s"): 2}), 2, None,
                     T(1, T(2, 1, 2, 0, 9)))]),
        # 6: context positioning format 3: t u → u raised (lookup 10).
        lookup(7, [T(3, 2, 1, coverage([g("t")]), coverage([g("u")]), 1, 10)]),
        # 7: chain context positioning format 1: k | l | z → l wider (11).
        lookup(8, [T(1, coverage([g("l")]), 1, T(1, T(1, g("k"), 1, 1, g("z"), 1, 0, 11)))]),
        # 8–11: nested single positionings.
        lookup(1, [pos1("n", 0x01, [100])]),
        lookup(1, [pos1("r", 0x04, [30])]),
        lookup(1, [pos1("u", 0x02, [40])]),
        lookup(1, [pos1("l", 0x04, [60])]),
    ]
    gpos = layout(gpos_lookups, {tag("kern"): [0, 1], tag("mark"): [2, 3], tag("dist"): [4, 5, 6, 7]})

    write_font(font, {"GDEF": gdef, "GSUB": gsub, "GPOS": gpos}, OUT)


def write_font(data, extra, path):
    n = struct.unpack(">H", data[4:6])[0]
    tables = {}
    for i in range(n):
        t, _, off, length = struct.unpack(">4sIII", data[12 + 16 * i:28 + 16 * i])
        tables[t.decode("latin-1")] = data[off:off + length]
    tables.update(extra)
    names = sorted(tables)
    out = bytearray(struct.pack(">IHHHH", 0x00010000, len(names), 0, 0, 0))
    body, offset = bytearray(), 12 + 16 * len(names)
    for name in names:
        t = tables[name]
        padded = t + b"\0" * (-len(t) % 4)
        checksum = sum(struct.unpack(">%dI" % (len(padded) // 4), padded)) & 0xFFFFFFFF
        out += struct.pack(">4sIII", name.encode("latin-1"), checksum, offset + len(body), len(t))
        body += padded
    with open(path, "wb") as f:
        f.write(out + body)


main()

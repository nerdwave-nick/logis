#!/usr/bin/env python3
"""Writes cases.json, the runs the HarfBuzz oracle shapes: the sample text
of tests/fonts/corpus.txt cut into the runs logis's line layout shapes,
then runs aimed at each part of the shaper.

    python3 -I internal/shape/testdata/cases.py
    python3 -I internal/shape/testdata/subset.py
    python3 -I internal/shape/testdata/synthetic.py
    python3 -I internal/shape/testdata/hbshape.py
"""
import json, os

HERE = os.path.dirname(os.path.abspath(__file__))
cases = []


def add(font, texts, rtl=False, **kw):
    for t in texts:
        c = {"font": font, "text": t}
        if rtl:
            c["rtl"] = True
        c.update(kw)
        cases.append(c)


corpus = open(os.path.join(HERE, "..", "..", "..", "tests", "fonts", "corpus.txt"), encoding="utf-8").read().split("\n")

# The pinned fonts of tests/fonts: the corpus and its words.
sans = "NotoSans-Regular.ttf"
add(sans, [corpus[0], corpus[1], "The", "machine", "spirit", "wakes:", "0123456789", " ",
           "Ça", "déjà", "vu,", "naïve", "Ærø", "—", "Œuvre,", "Šťastný", "logis:", "and", "(42)", "ok",
           "Praise", "the", "Omnissiah", "office fit", "fi", "ffi", "AVATAR Wave", "Ç vs Ç",
           "é ä ç", "MMMMMMMMMMMMMMMMMMMM", "…", "do-not-disturb", "on", "rite of oil",
           "12/345", "Hail", "IIII", "Ave", "INVOKE", "a‍b", "a­b", "x\ty"])
add(sans, ["(42)", "(42) ok", "[x] <y> {z}"], rtl=True)
add("NotoSansHebrew-Regular.ttf", ["שָׁלוֹם", "שלום", "שָׁלוֹם and"], rtl=True)
add("NotoSansArabic-Regular.ttf", ["مرحبا", "بالعالم", "مرحبا بالعالم", "مرحبا بالعالم (42)"], rtl=True)
add("NotoSansCJKjp-Regular.otf", [corpus[2], "機械の精霊", "万机之神", "기계", "정령"])
emoji = "NotoColorEmoji.ttf"
add(emoji, ["🔥⚙️💀🤖", "🔥", "⚙️", "💀", "🤖", "👍🏽", "🧑‍💻", corpus[4]])
add("NotoSansMono-Regular.ttf", ["do-not-disturb\ton", "rite of oil", "INVOKE"])
add("NotoSerif-SemiBold.ttf", ["INVOKE", "Hail"])

# System fonts cut down to these runs (subset.py).
full = "NotoSans-Full.ttf"
add(full, [corpus[0], corpus[1], "office fit ffl ffi fj", "Tiếng Việt có dấu", "ǻ Ǻ ḗ ẫ ỗ ự",
           "ẹ́ x̣́ q̣̇ ą́", "Ǻ ȫ",
           "AV Wa Ta Yo T. P, F. LT", "Ελληνικά άέήί ΐ", "Русский язык й ё Ӂ",
           "a‍b a‌b", "soft­hyphen", "x͏́ x́͏̀",
           "em space thin space figure  narrow  hair  quad ",
           "non‑breaking", "1/2 3⁄4 12⁄345", "ﬁ ﬂ", "i̇ İ ı",
           "‘quotes’ “double”", "à́̂̃̄̅̆",
           "ẛ̣", "x́ͅ", "Å Å Å", "Ω Ω"])
add(full, ["(a)", "a<b>c"], rtl=True)
add(full, ["Hail AVATAR"], features=["aalt=2"])
add(full, ["Hail office"], features=["smcp", "-liga"])
add(full, ["1/2 1234"], features=["frac"])
add("NotoSans-NoGPOS.ttf", ["ẹ́ x̣́ ą́", "Ǻ ȫ i̇",
                            "office ﬁ̣́", "Tiếng Việt"])
ar = "NotoSansArabic-Full.ttf"
add(ar, ["مرحبا بالعالم", "لا إله إلا الله", "بِسْمِ اللَّهِ الرَّحْمَٰنِ الرَّحِيمِ", "فارسی گچپژ",
         "ـــ بـــ", "عربي 123", "١٢٣ ٤٥٦", "ال‍ا", "ل‌ا",
         "(مرحبا)", "أَهْلًا", "لَٔ", "بَٔ", "بَّٕٔ",
         "سلام‍سلام", "ک‍ے", "ب", "بب", "ببب", "ع ع",
         "لَّا", "لاً"], rtl=True)
add(ar, ["123", "بب"])
he = "NotoSansHebrew-Full.ttf"
add(he, ["שָׁלוֹם עֲלֵיכֶם", "בְּרֵאשִׁית בָּרָא", "יִשְׂרָאֵל", "בְַֽ",
         "בִָֽ", "שּׁ", "יִ", "(שלום)", "עִבְרִית 123"], rtl=True)
add("NotoNastaliqUrdu-Subset.ttf", ["اردو زبان", "پاکستان", "نستعلیق", "خوش آمدید", "بہت شکریہ"], rtl=True)
add("NotoSansSyriac-Subset.ttf", ["ܫܠܡܐ ܥܠܡܐ", "ܐܠܗܐ", "ܡܪܝܐ", "ܐܕܪ"], rtl=True)
em = "NotoColorEmoji-Subset.ttf"
add(em, ["👍🏽", "🧑‍💻", "👨‍👩‍👧‍👦", "🇩🇪🇺🇸", "1️⃣", "#️⃣", "⚙️", "⚙︎", "⚙", "❤️", "☺️",
         "🏳️‍🌈", "🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f", "👋🏻👋🏿",
         "🔥⚙️💀🤖", "👩🏽‍🚀", "🧑‍", "‍💻"])
adw = "AdwaitaSans-Subset.ttf"
for ppem in (0, 12, 16):
    add(adw, ["AVATAR Tava Wo Ty.", "Ť Ľ ď"], **({"ppem": ppem} if ppem else {}))
add(adw, ["Hail 1/2"], features=["salt"])
add("AdwaitaMono-Subset.ttf", ["--> <-- => === !== ->> <!-- ||| ::: www <<< >>= ...",
                                "fi fl /* */ // ## ~~ ;; 0x1F", "a︀ ⨁"])

# The synthetic font (synthetic.py), at several ppem for its device tables.
syn = "Synthetic-Layout.ttf"
texts = ["ab", "ba", "cd", "dc", "xef", "ef", "g", "ihj", "hj", "ih", "fi", "f́ị",
         "f̣í", "fị́", "pq", "pqq", "vwy", "To", "Tox", "mn", "nm", "rs", "sr",
         "tu", "ut", "klz", "kl", "ạ́", "ẹ́", "ó"]
for ppem in (0, 9, 10, 11, 12, 13, 14, 15):
    add(syn, texts, **({"ppem": ppem} if ppem else {}))
add(syn, ["ab cd xef g ihj"], rtl=True)

with open(os.path.join(HERE, "cases.json"), "w", encoding="utf-8") as f:
    json.dump(cases, f, ensure_ascii=False, indent=0)
    f.write("\n")
print(len(cases), "cases")

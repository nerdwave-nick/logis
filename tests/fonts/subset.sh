#!/bin/sh
# Regenerates the pinned test fonts from the Noto fonts installed on an Arch
# machine (noto-fonts 2026.10.01, noto-fonts-cjk 20240730, noto-fonts-emoji
# 2.051). The subsets keep the outlines, hinting, layout features and the
# copyright/licence names of the originals, so the goldens in
# tests/goldens/text/ render the same bytes on every machine and in CI.
#
# Needs fonttools (run here through uvx; nothing is installed system-wide).
# All fonts are SIL OFL 1.1, see OFL.txt. Changing a font changes the goldens:
# rerun with LOGIS_UPDATE_GOLDENS=1 cargo test --test text, then review them.
set -eu
cd "$(dirname "$0")"
src=/usr/share/fonts
subset() {
    uvx --from fonttools pyftsubset "$@" \
        --layout-features='*' --notdef-outline --name-IDs=0,1,2,3,4,5,6,13,14
}
# Latin: ASCII, Latin-1, Latin Extended-A and general punctuation (… — ’).
subset "$src/noto/NotoSans-Regular.ttf" --output-file=NotoSans-Regular.ttf \
    --unicodes=U+0020-007E,U+00A0-017F,U+2010-2027
# The other scripts: only what the corpus uses.
subset "$src/noto/NotoSansHebrew-Regular.ttf" --output-file=NotoSansHebrew-Regular.ttf \
    --text-file=corpus.txt
subset "$src/noto/NotoSansArabic-Regular.ttf" --output-file=NotoSansArabic-Regular.ttf \
    --text-file=corpus.txt
subset "$src/noto-cjk/NotoSansCJK-Regular.ttc" --font-number=0 \
    --output-file=NotoSansCJKjp-Regular.otf --text-file=corpus.txt
subset "$src/noto/NotoColorEmoji.ttf" --output-file=NotoColorEmoji.ttf \
    --text-file=corpus.txt

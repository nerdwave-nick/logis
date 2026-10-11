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
# Latin: ASCII, Latin-1, Latin Extended-A and general punctuation (… — ’);
# Greek (with the polytonic Greek Extended block) and Cyrillic for the debug
# panel's rows, which the E2E goldens show.
subset "$src/noto/NotoSans-Regular.ttf" --output-file=NotoSans-Regular.ttf \
    --unicodes=U+0020-007E,U+00A0-017F,U+2010-2027,U+0370-03FF,U+0400-04FF,U+1F00-1FFF
# The other scripts: only what the corpus uses.
subset "$src/noto/NotoSansHebrew-Regular.ttf" --output-file=NotoSansHebrew-Regular.ttf \
    --text-file=corpus.txt
subset "$src/noto/NotoSansArabic-Regular.ttf" --output-file=NotoSansArabic-Regular.ttf \
    --text-file=corpus.txt
subset "$src/noto-cjk/NotoSansCJK-Regular.ttc" --font-number=0 \
    --output-file=NotoSansCJKjp-Regular.otf --text-file=corpus.txt
subset "$src/noto/NotoColorEmoji.ttf" --output-file=NotoColorEmoji.ttf \
    --text-file=corpus.txt
# The generic families the bundled themes fall back to when their own faces
# (IBM Plex Mono, Cinzel, VT323, Terminus) are not installed: monospace and
# serif, Latin plus the shade blocks of the phosphor theme's pending mark.
# These keep the typographic family names (IDs 16, 17) too, so a SemiBold
# face is "Noto Serif" at weight 600, as installed fonts are.
for face in NotoSansMono-Regular NotoSansMono-SemiBold NotoSerif-SemiBold; do
    subset "$src/noto/$face.ttf" --output-file="$face.ttf" --name-IDs=0,1,2,3,4,5,6,13,14,16,17 \
        --unicodes=U+0020-007E,U+00A0-017F,U+2010-2027,U+2591-2593
done

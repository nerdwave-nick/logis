#!/bin/sh
# A fixture provider for logis's provider protocol (docs/provider-protocol.md),
# in plain POSIX sh so the tests need nothing else. `forge.sh MODE`:
#
#   flow      rite → aspect → toast + close. The rite stage starts with
#             ">rite of oil" pending (it settles at once, or once the file
#             $FORGE_GATE exists); activating it shows the aspect stage;
#             activating an aspect sends a success toast and closes.
#   act       one settled item; activating it starts an "action" (a long
#             sleep, its pid in $FORGE_DIR/action) and then the provider
#             idles on, deaf (its pid in $FORGE_DIR/provider).
#   stubborn  one stage, then ignores cancel, end of input and SIGTERM
#             (its pid in $FORGE_DIR/provider).
#   mute      says nothing at all.
#   garbage   greets, then writes a line that is not JSON.
#   departs   greets, then exits 3.
#
# Every line it hears is appended to $FORGE_DIR/heard, after its working
# directory and $FORGE_TOKEN (to show it runs as its caller would).
set -u
dir=${FORGE_DIR:-/tmp}
say() { printf '%s\n' "$1"; }
heard() { printf '%s\n' "$1" >> "$dir/heard"; }

printf 'cwd %s\ntoken %s\n' "$(pwd)" "${FORGE_TOKEN:-}" >> "$dir/heard"
case $1 in
mute) exec sleep 30 </dev/null >/dev/null ;;
esac

read -r line || exit 0
heard "$line"
say '{"type":"hello","protocol":1}'

case $1 in
garbage) say 'the machine spirit is displeased'; exec cat >/dev/null ;;
departs) exit 3 ;;
esac

rite='{"type":"stage","id":"rite","prompt":"rite: ","placeholder":"choose a rite","width":44,"lines":6,"items":[
  {"id":"oil","columns":[">rite of oil","unguent"],"glyph":"⚙","badges":["current"],"pending":true},
  {"id":"fire","columns":["rite of ignition","spark"],"glyph":"†"}]}'
case $1 in
flow) say "$(printf '%s' "$rite" | tr -d '\n')"
      settle='{"type":"items","items":[{"id":"oil","columns":[">rite of oil","unguent"],"glyph":"⚙","badges":["current"]}]}'
      if [ -n "${FORGE_GATE:-}" ]; then
          ( while [ ! -e "$FORGE_GATE" ]; do sleep 0.05; done; say "$settle" ) &
      else
          say "$settle"
      fi ;;
act) say '{"type":"stage","id":"rite","items":[{"id":"oil","columns":[">rite of oil"]}]}' ;;
stubborn) echo $$ > "$dir/provider"
          say '{"type":"stage","id":"rite","items":[{"id":"oil","columns":["rite of oil"]}]}'
          trap '' TERM
          while :; do sleep 0.1; done ;;
esac

while read -r line; do
    heard "$line"
    case $1:$line in
    flow:*'"activate","stage":"rite","id":"oil"'*)
        say '{"type":"stage","id":"aspect","prompt":"aspect: ","items":[{"id":"on","columns":[">anointed"],"badges":["current"]},{"id":"off","columns":["dry"]}]}' ;;
    flow:*'"activate","stage":"aspect"'*)
        say '{"type":"toast","text":"the rite of oil is begun","level":"success"}'
        say '{"type":"close"}'
        exit 0 ;;
    act:*'"activate"'*)
        sleep 30 </dev/null >/dev/null 2>&1 &
        echo $! > "$dir/action"
        echo $$ > "$dir/provider"
        exec sleep 30 </dev/null >/dev/null ;;
    *'"cancel"'*) exit 0 ;;
    esac
done

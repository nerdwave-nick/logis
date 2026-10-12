#!/bin/zsh -f
# Prints what zsh's completion system offers for a command line, one
# candidate per line: zsh -f zsh-capture.zsh FPATH_DIR 'logis --dm'
#
# zsh completes only inside its line editor, so this drives an interactive
# `zsh -f -i` on a pseudo-terminal (zsh/zpty), presses Tab after the line,
# and records each candidate as compadd receives it. The technique follows
# Valodim's zsh-capture-completion (MIT); used by tests/completions.rs.

zmodload zsh/zpty || exit 3
zpty z zsh -f -i
zpty -w z "fpath=(${(q)1} \$fpath); autoload -U compinit; compinit -u -D"
# A NUL line before and after completion marks where the candidates are;
# then the shell exits.
zpty -w z "null-line () { echo -E - \$'\\0' }"
zpty -w z "compprefuncs=( null-line )"
zpty -w z "comppostfuncs=( null-line exit )"
zpty -w z "zstyle ':completion:*' list-grouped false"
zpty -w z "zstyle ':completion:*' insert-tab false"
zpty -w z "zstyle ':completion:*' list-separator ''"
# compadd prints each candidate; a compadd only asked to fill an array or
# to match (-O, -A, -D) runs for real.
zpty -w z 'compadd () {
  if [[ ${@[1,(i)(-|--)]} == *-(O|A|D)\ * ]]; then
    builtin compadd "$@"
    return $?
  fi
  typeset -a __hits
  builtin compadd -A __hits "$@"
  setopt localoptions norcexpandparam extendedglob
  typeset -A apre hpre hsuf asuf
  zparseopts -E P:=apre p:=hpre S:=asuf s:=hsuf
  local hit
  for hit in $__hits; do
    echo -E - $IPREFIX$apre$hpre$hit$hsuf$asuf
  done
}'
zpty -w z "bindkey '^I' complete-word"
# The line and a Tab, without a newline: the line is never run.
zpty -n -w z "$2"$'\t'

integer marks=0
local line
while zpty -r z; do :; done | while IFS= read -r line; do
  if [[ $line == *$'\0\r' ]]; then
    (( marks++ )) && exit 0
    continue
  fi
  (( marks )) && echo -E - "${line%$'\r'}"
done

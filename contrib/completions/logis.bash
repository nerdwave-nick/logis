# bash completion for logis(1), the cogitator of the Machine God.
# Maintained by hand; tests/completions.rs pins it to the runes the parser
# (src/cli/) accepts. Installed as
# /usr/share/bash-completion/completions/logis.

_logis() {
    local cur=${COMP_WORDS[COMP_CWORD]}
    local prev=
    ((COMP_CWORD > 0)) && prev=${COMP_WORDS[COMP_CWORD - 1]}
    # `--rune=value`: bash splits at '=' (COMP_WORDBREAKS).
    if [[ $cur == = ]]; then
        cur=
    elif [[ $prev == = ]] && ((COMP_CWORD > 1)); then
        prev=${COMP_WORDS[COMP_CWORD - 2]}
    fi
    COMPREPLY=()

    # Everything after --provider belongs to the provider's program.
    local i
    for ((i = 1; i < COMP_CWORD; i++)); do
        if [[ ${COMP_WORDS[i]} == --provider ]]; then
            if ((i + 1 == COMP_CWORD)); then
                mapfile -t COMPREPLY < <(compgen -c -- "$cur" | sort -u)
            else
                mapfile -t COMPREPLY < <(compgen -f -- "$cur")
            fi
            return
        fi
    done

    case $prev in
        --config)
            mapfile -t COMPREPLY < <(compgen -f -- "$cur")
            return
            ;;
        --prompt | -p | --prompt-only | --placeholder | --lines | -l | --width | -w | \
            --search | --select | --select-index | --with-nth | --accept-nth | \
            --match-nth | --nth-delimiter | --idle-timeout)
            # A value of the caller's own choosing.
            return
            ;;
    esac

    if [[ ${COMP_WORDS[1]} == daemon ]] && ((COMP_CWORD > 1)); then
        mapfile -t COMPREPLY < <(compgen -W '--idle-timeout --config --detach --debug-panel' -- "$cur")
        return
    fi

    if ((COMP_CWORD == 1)) && [[ $cur != -* ]]; then
        mapfile -t COMPREPLY < <(compgen -W 'daemon' -- "$cur")
        return
    fi

    local modes='--dmenu -d --dmenu0 --apps --servitor --provider --check-config --version -V'
    local runes='--prompt -p --prompt-only --placeholder --password --lines -l --width -w
        --counter --search --select --select-index --only-match --no-run-if-empty -R
        --index --with-nth --accept-nth --match-nth --nth-delimiter -i'
    mapfile -t COMPREPLY < <(compgen -W "$modes $runes --config" -- "$cur")
}

complete -F _logis logis

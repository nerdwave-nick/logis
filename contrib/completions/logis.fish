# fish completion for logis(1), the cogitator of the Machine God.
# Maintained by hand; cmd/logis/completions_test.go pins it to the runes the
# parser (cmd/logis/cli.go) accepts. Installed as
# /usr/share/fish/vendor_completions.d/logis.fish.

# Whether the command line so far is `logis daemon ...`.
function __logis_daemon
    set -l words (commandline -opc)
    test (count $words) -ge 2; and test "$words[2]" = daemon
end

# Whether --provider came before: the rest belongs to its program.
function __logis_provider
    contains -- --provider (commandline -opc)
end

# Whether no mode rune came before (the modes exclude each other).
function __logis_no_mode
    not __fish_seen_argument -s d -s V -l dmenu -l dmenu0 -l apps -l servitor -l provider -l check-config -l version
end

complete -c logis -f

# What follows --provider is the provider's own command line.
complete -c logis -n __logis_provider -F -a '(__fish_complete_subcommand -- --config)'

# The daemon itself.
complete -c logis -n 'test (count (commandline -opc)) -eq 1' -a daemon -d 'The machine spirit itself'
complete -c logis -n __logis_daemon -l idle-timeout -x -d 'Sleep once no client has called this long'
complete -c logis -n __logis_daemon -l detach -d 'Leave the stdout and stderr of its starter once listening'
complete -c logis -n __logis_daemon -l debug-panel -d 'Show the static debug panel of the renderer instead'

set -l client 'not __logis_daemon; and not __logis_provider'
set -l mode "$client; and __logis_no_mode"

complete -c logis -n "not __logis_provider" -l config -r -F -d 'The config to read'

# Modes.
complete -c logis -n $mode -s d -l dmenu -d 'Pick a line from stdin'
complete -c logis -n $mode -l dmenu0 -d 'Pick a NUL-ended record from stdin'
complete -c logis -n $mode -l apps -d 'The app launcher (also plain logis)'
complete -c logis -n $mode -l servitor -d 'Choose a rite of servitor and invoke it'
complete -c logis -n $mode -l provider -d 'Show what a provider program says'
complete -c logis -n $mode -l check-config -d 'Judge the config and its theme'
complete -c logis -n $mode -s V -l version -d 'Print the name and version'

# The list runes (with --dmenu or --dmenu0).
complete -c logis -n $client -s p -l prompt -x -d 'Shown before the query'
complete -c logis -n $client -l prompt-only -x -d 'A prompt alone, no list'
complete -c logis -n $client -l placeholder -x -d 'Shown while the query is empty'
complete -c logis -n $client -l password -d 'Show each typed character as * (or --password=C)'
complete -c logis -n $client -s l -l lines -x -d 'Rows shown at most'
complete -c logis -n $client -s w -l width -x -d 'Window width in characters'
complete -c logis -n $client -l counter -d 'Show how many items match'
complete -c logis -n $client -l search -x -d 'The query to open with'
complete -c logis -n "$client; and not __fish_seen_argument -l select-index" -l select -x -d 'Preselect the item with this text'
complete -c logis -n "$client; and not __fish_seen_argument -l select" -l select-index -x -d 'Preselect item N, from 0'
complete -c logis -n $client -l only-match -d 'Accept only an item, never typed text'
complete -c logis -n $client -s R -l no-run-if-empty -d 'Exit at once on an empty stdin'
complete -c logis -n $client -l index -d 'Print the chosen index instead of its text'
complete -c logis -n $client -l with-nth -x -d 'The columns shown'
complete -c logis -n $client -l accept-nth -x -d 'The columns printed'
complete -c logis -n $client -l match-nth -x -d 'The columns matched'
complete -c logis -n $client -l nth-delimiter -x -d 'The column delimiter (tab)'
complete -c logis -n $client -s i -d 'Ignored: logis always ignores case'

#!/usr/bin/env bash
set -eu
# Called from integration.sh, using its isolated state and broken Lua config.
eval "$("$SUP" completion bash)"
check() {
    local want=$1 actual
    shift
    COMP_WORDS=("$SUP" "$@")
    COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
    _sup_complete
    # Bash 3.2 treats an empty array as unset under nounset.
    actual=$(printf '%s\n' "${COMPREPLY[@]-}")
    if [[ $actual != "$want" ]]; then
        printf 'completion for %s: expected <%s>, got <%s>\n' "$*" "$want" "$actual" >&2
        exit 1
    fi
}
check docker-docs rm docker-
check docker-docs docker-
check docker-docs rm -f docker-
check docker-docs -d docker-
check '' rm docker-docs ''
check '' rm --arg ''
check '' --kit ''
check '' rm --pr ''
check docker-docs rm --pr 123 docker-
check docker-docs rm --pr=123 docker-
check --force rm --f
check bash completion ''
check '' ls docker-
check '' args docker-
check '' kits docker-
check --config kits --c
check $'args\ncompletion\nkits\nls\nrm\ndocker-docs' ''
check "$XDG_CONFIG_HOME/sup/config.lua" --config "$XDG_CONFIG_HOME/sup/config.l"

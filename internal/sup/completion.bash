# Bash 3.2+; load with: eval "$(sup completion bash)"
_sup_complete() {
    local cur word command='' target='' value='' candidate i
    COMPREPLY=()
    cur=${COMP_WORDS[COMP_CWORD]}
    for ((i=1; i<COMP_CWORD; i++)); do
        word=${COMP_WORDS[i]}
        if [[ -n $value ]]; then
            value=''
            continue
        fi
        if ((i == 1)); then
            case $word in
                open|stop|recreate|inspect|rm|ls|completion) command=$word; continue ;;
            esac
        fi
        case $word in
            -d|--detached|-f|--force|--plan|-h|--help|--json|--verbose) ;;
            --*=*) ;;
            --*) value=$word ;;
            -*) ;;
            *) target=$word ;;
        esac
    done
    if [[ -n $value ]]; then
        if [[ $value == --via ]]; then
            COMPREPLY=($(compgen -W 'terminal ssh herdr' -- "$cur"))
        elif [[ $value == --agent ]]; then
            COMPREPLY=($(compgen -W 'codex claude shell' -- "$cur"))
        fi
        return 0
    fi
    local words=''
    case $command in
        completion) [[ -z $target ]] && words='bash' ;;
        ls) words='--json -h --help' ;;
        stop) words='--verbose -h --help' ;;
        inspect) words='-h --help' ;;
        open) words='--verbose --via --agent --plan --detached -h --help' ;;
        recreate) words='--verbose --via --agent --plan --force --detached -h --help' ;;
        rm) words='--verbose -f --force -h --help' ;;
        '') words='--verbose -d --detached --via --agent --plan -h --help'
            if ((COMP_CWORD == 1)) && [[ $cur != -* ]]; then
                words='completion inspect ls open recreate rm stop'
            fi ;;
    esac
    if [[ $cur == -* || $command == completion ]] || ((COMP_CWORD == 1)); then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <(compgen -W "$words" -- "$cur")
    fi
    if [[ -z $target && $cur != -* && ( -z $command || $command == rm || $command == open || $command == recreate || $command == stop || $command == inspect ) ]]; then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <("${COMP_WORDS[0]}" __complete "$cur" 2>/dev/null)
    fi
    return 0
}
complete -F _sup_complete sup

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
                open|stop|recreate|inspect|history|rm|ls|completion) command=$word; continue ;;
            esac
        fi
        case $word in
            -d|--detached|-f|--force|--plan|-h|--help|--no-history|--json|--yes|-y|--auto-approve) ;;
            --*=*) ;;
            --*) value=$word ;;
            -*) ;;
            *) if [[ $command == history && -z $target ]]; then command="history-$word"; else target=$word; fi ;;
        esac
    done
    if [[ -n $value ]]; then
        if [[ $value == --env-file ]]; then
            while IFS= read -r candidate; do
                COMPREPLY+=("$candidate")
            done < <(compgen -f -- "$cur")
            compopt -o filenames 2>/dev/null || :
        elif [[ $value == --via ]]; then
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
        history) words='path clear' ;;
        history-clear) words='--yes -h --help' ;;
        history-path|stop|inspect) words='-h --help' ;;
        open|recreate) words='--via --agent --env-file --env-arg --kit --cwd --plan --force --auto-approve --detached -h --help' ;;
        rm) words='-f --force -h --help' ;;
        '') words='-d --detached --kit --name --env-file --env-arg --via --agent --cwd --no-history --auto-approve --plan -h --help'
            if ((COMP_CWORD == 1)) && [[ $cur != -* ]]; then
                words='completion history inspect ls open recreate rm stop'
            fi ;;
    esac
    if [[ $cur == -* || $command == completion || $command == history ]] || ((COMP_CWORD == 1)); then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <(compgen -W "$words" -- "$cur")
    fi
    if [[ -z $target && $cur != -* && ( -z $command || $command == rm || $command == open || $command == recreate || $command == stop || $command == inspect || $command == history-* ) ]]; then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <("${COMP_WORDS[0]}" __complete "$cur" 2>/dev/null)
    fi
    return 0
}
complete -F _sup_complete sup

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
                rm|ls|args|completion) command=$word; continue ;;
            esac
        fi
        case $word in
            -d|--detached|-f|--force|--plan|-h|--help) ;;
            --*=*) ;;
            --*|-a) value=$word ;;
            -*) ;;
            *) target=$word ;;
        esac
    done
    if [[ -n $value ]]; then
        if [[ $value == --config ]]; then
            while IFS= read -r candidate; do
                COMPREPLY+=("$candidate")
            done < <(compgen -f -- "$cur")
            compopt -o filenames 2>/dev/null || :
        fi
        return 0
    fi
    local words=''
    case $command in
        completion) [[ -z $target ]] && words='bash' ;;
        ls) words='-h --help' ;;
        args) words='--config -h --help' ;;
        rm) words='-a --arg --name -f --force -h --help' ;;
        '') words='-d --detached --kit --name -a --arg --config --plan -h --help'
            if ((COMP_CWORD == 1)) && [[ $cur != -* ]]; then
                words='args completion ls rm'
            fi ;;
    esac
    if [[ $cur == -* || $command == completion ]] || ((COMP_CWORD == 1)); then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <(compgen -W "$words" -- "$cur")
    fi
    if [[ -z $target && $cur != -* && ( -z $command || $command == rm ) ]]; then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <("${COMP_WORDS[0]}" __complete "$cur" 2>/dev/null)
    fi
    return 0
}
complete -F _sup_complete sup

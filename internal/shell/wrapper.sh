# gibbon shell integration. Install with:  eval "$(gibbon shell-init bash)"
# The wrapper lets `gibbon feat -c`, `feat switch` and `feat prune` change the
# current directory. Everything else is forwarded unchanged.
gibbon() {
    local __gibbon_cd_file __gibbon_rc __gibbon_target
    __gibbon_cd_file="$(mktemp "${TMPDIR:-/tmp}/gibbon-cd.XXXXXX")" || return 1
    GIBBON_CD_FILE="$__gibbon_cd_file" command gibbon "$@"
    __gibbon_rc=$?
    __gibbon_target=""
    if [ -s "$__gibbon_cd_file" ]; then
        IFS= read -r __gibbon_target < "$__gibbon_cd_file"
    fi
    rm -f "$__gibbon_cd_file"
    if [ -n "$__gibbon_target" ] && [ -d "$__gibbon_target" ]; then
        cd "$__gibbon_target" || return 1
    fi
    return $__gibbon_rc
}

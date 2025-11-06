# Persistent bash history
export HISTFILE=/root/.cache/.bash_history
export HISTSIZE=50000
export HISTFILESIZE=100000
export HISTIGNORE=exit
export HISTCONTROL=ignoredups
export HISTTIMEFORMAT='%F %T '

# Interactive shell settings
if [[ $- == *i* ]]; then
    shopt -s histappend
    PROMPT_COMMAND="history -a; ${PROMPT_COMMAND}"
fi

export CONTAINERD_ADDRESS="/var/lib/miren/containerd/containerd.sock"
export CONTAINERD_NAMESPACE="miren"
export OTEL_SDK_DISABLED=true

# Less configuration
export LESSCHARSET=utf-8
export LESS='-R'

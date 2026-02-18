#compdef sprout

_sprout() {
  local -a commands
  commands=(
    'ui:open interactive interface'
    'new:create a branch + worktree'
    'list:list worktrees'
    'go:jump to a worktree'
    'path:print worktree path'
    'launch:open tmux tools for worktree'
    'detach:close tmux session for a worktree'
    'agent:start/stop/attach agent for worktree'
    'rm:remove a worktree'
    'doctor:check tool and repo health'
    'shell-hook:print shell integration script'
    'version:print version'
    'help:show help'
  )

  local context state line
  _arguments -C \
    '1:command:->command' \
    '*::arg:->args'

  case "$state" in
    command)
      _describe 'command' commands
      ;;
    args)
      case "$words[2]" in
        new)
          _arguments \
            '1:type:(feat fix chore docs refactor test)' \
            '2:name:_message "branch name"' \
            '--from[base branch]:branch:_message "branch"' \
            '--no-launch[do not launch tmux tools]'
          ;;
        list)
          _arguments '--json[output as json]'
          ;;
        go)
          _arguments \
            '1:target:_message "branch or path"' \
            '--attach[attach tmux when outside]' \
            '--no-launch[do not create/focus tmux window]'
          ;;
        launch)
          _arguments \
            '1:target:_message "branch or path"' \
            '--no-attach[do not attach tmux when outside]'
          ;;
        detach)
          _arguments '1:target:_message "branch or path"'
          ;;
        agent)
          _arguments \
            '1:action:(start stop attach)' \
            '2:target:_message "branch or path"'
          ;;
        rm)
          _arguments \
            '1:target:_message "branch or path"' \
            '--delete-branch[delete local branch too]' \
            '--force[force remove dirty worktree]'
          ;;
        path)
          _arguments '1:target:_message "branch or path"'
          ;;
        shell-hook)
          _arguments '1:shell:(zsh bash fish)'
          ;;
      esac
      ;;
  esac
}

_sprout "$@"

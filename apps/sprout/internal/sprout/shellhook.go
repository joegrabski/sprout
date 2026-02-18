package sprout

import "fmt"

func ShellHook(shell string) (string, error) {
	switch shell {
	case "zsh", "bash":
		return `spr() {
  local _out _rc _cd
  _out="$(SPROUT_EMIT_CD_MARKER=1 command sprout "$@")"
  _rc=$?

  _cd="$(printf '%s\n' "$_out" | sed -n 's/^__SPROUT_CD__=//p' | tail -n 1)"

  if [[ -n "$_out" ]]; then
    printf '%s\n' "$_out" | sed '/^__SPROUT_CD__=/d'
  fi

  if [[ -n "$_cd" ]]; then
    cd "$_cd" || return
  fi

  return $_rc
}
`, nil
	case "fish":
		return `function spr
  set -l _out (env SPROUT_EMIT_CD_MARKER=1 command sprout $argv)
  set -l _rc $status
  set -l _cd ""

  for line in $_out
    if string match -qr '^__SPROUT_CD__=' -- $line
      set _cd (string replace '__SPROUT_CD__=' '' -- $line)
    else
      echo $line
    end
  end

  if test -n "$_cd"
    cd "$_cd"
  end

  return $_rc
end
`, nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

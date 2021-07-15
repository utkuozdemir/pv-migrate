package app

import (
	"fmt"
	"github.com/urfave/cli/v2"
)

const (
	commandCompletion = "completion"
	flagBash          = "bash"
	flagZsh           = "zsh"

	// completionCodeBash is adapted from https://github.com/urfave/cli/blob/master/autocomplete/bash_autocomplete
	completionCodeBash = `
#! /bin/bash
PROG=pv-migrate
: ${PROG:=$(basename ${BASH_SOURCE})}

_cli_bash_autocomplete() {
  if [[ "${COMP_WORDS[0]}" != "source" ]]; then
    local cur opts base
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    if [[ "$cur" == "-"* ]]; then
      opts=$( ${COMP_WORDS[@]:0:$COMP_CWORD} ${cur} --generate-bash-completion )
    else
      opts=$( ${COMP_WORDS[@]:0:$COMP_CWORD} --generate-bash-completion )
    fi
    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
    return 0
  fi
}

complete -o bashdefault -o default -o nospace -F _cli_bash_autocomplete $PROG
unset PROG`

	// completionCodeZsh is adapted from https://github.com/urfave/cli/blob/master/autocomplete/zsh_autocomplete
	completionCodeZsh = `
PROG=pv-migrate
#compdef $PROG

_cli_zsh_autocomplete() {

  local -a opts
  local cur
  cur=${words[-1]}
  if [[ "$cur" == "-"* ]]; then
    opts=("${(@f)$(_CLI_ZSH_AUTOCOMPLETE_HACK=1 ${words[@]:0:#words[@]-1} ${cur} --generate-bash-completion)}")
  else
    opts=("${(@f)$(_CLI_ZSH_AUTOCOMPLETE_HACK=1 ${words[@]:0:#words[@]-1} --generate-bash-completion)}")
  fi

  if [[ "${opts[1]}" != "" ]]; then
    _describe 'values' opts
  else
    _files
  fi

  return
}

compdef _cli_zsh_autocomplete $PROG
unset PROG`
)

var (
	completionCommand = cli.Command{
		Name:  commandCompletion,
		Usage: "Output shell completion code for the specified shell (bash or zsh)",
		Subcommands: []*cli.Command{
			{
				Name: flagBash,
				Action: func(context *cli.Context) error {
					fmt.Println(completionCodeBash)
					return nil
				},
			},
			{
				Name: flagZsh,
				Action: func(context *cli.Context) error {
					fmt.Println(completionCodeZsh)
					return nil
				},
			},
		},
	}
)

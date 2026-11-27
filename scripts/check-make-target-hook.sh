#!/usr/bin/env bash
# PreToolUse hook on Bash: refuses a command that runs one of the full checks
# CI runs and an agent should not run before a push, naming the -changed
# target to run instead. CHECKS=full anywhere in the command lets it through.
# The Makefile prints the same advice to a human who runs one (`note: ...`).
#
# The command is split into the segments the shell would run, with quotes,
# heredoc bodies and a `bash -c` argument parsed as the shell parses them, so
# a target named in a grep, a commit message or a file being written passes.
set -euo pipefail

command=$(jq -r '.tool_input.command // empty')

reasons=()
override=

# advise <target> <counterpart>
advise() {
  reasons+=("CI runs $1. Before a push, run make $2; to reproduce a CI failure, rerun with CHECKS=full.")
}

# check_make <arg>...: the arguments after `make`, assignments among them.
check_make() {
  local full=$prefix_full targets=() word
  while [ $# -gt 0 ]; do
    word=$1
    shift
    case "$word" in
      FULL=1) full=1 ;;
      *=*) ;;
      -C | -f | -I | -o | -W | --file | --directory) shift || true ;;
      -j | -l) [[ ${1:-} =~ ^[0-9]+$ ]] && shift ;;
      -n | --dry-run | --just-print | --recon) return 0 ;;
      -*) ;;
      *) targets+=("$word") ;;
    esac
  done
  for word in "${targets[@]}"; do
    case "$word" in
      vet-desktop | vet-desktop-sidecar | vet-desktop-host | vet-desktop-webview) advise "$word" vet-desktop-changed ;;
      test-desktop | test-desktop-sidecar | test-desktop-host | test-desktop-webview) advise "$word" test-desktop-changed ;;
      test-e2e) reasons+=("CI runs test-e2e. Before a push, run make lint-e2e and the tests you changed; to reproduce a CI failure, rerun with CHECKS=full.") ;;
      cover-*-changed)
        if [ "$full" = 1 ]; then
          reasons+=("CI measures the whole area. Before a push, run make $word without FULL=1; to reproduce a CI failure, rerun with CHECKS=full.")
        fi
        ;;
    esac
  done
  return 0
}

# check_segment <word>...: one simple command.
check_segment() {
  local prefix_full=
  while [ $# -gt 0 ]; do
    case "$1" in
      FULL=1) prefix_full=1 ;;
      *=*) ;;
      env | time | command | exec | nohup) ;;
      *) break ;;
    esac
    shift
  done
  [ $# -gt 0 ] || return 0
  case "${1##*/}" in
    make | gmake)
      shift
      check_make "$@"
      ;;
    bash | sh | zsh)
      shift
      while [ $# -gt 0 ]; do
        case "$1" in
          -*c*)
            [ $# -gt 1 ] && scan "$2"
            return 0
            ;;
          -*) shift ;;
          *) return 0 ;;
        esac
      done
      ;;
  esac
}

# scan <string>: splits a shell command into simple commands and checks each.
scan() {
  local s=$1 n=${#1} i=0 c quote='' word='' in_word='' words=() heredocs=() delim strip line
  while [ $i -le "$n" ]; do
    c=${s:i:1}
    i=$((i + 1))
    if [ "$quote" = "'" ]; then
      if [ "$c" = "'" ]; then quote=; else word+=$c; fi
      continue
    fi
    if [ "$quote" = '"' ]; then
      case "$c" in
        '"') quote= ;;
        \\) word+=${s:i:1} && i=$((i + 1)) ;;
        *) word+=$c ;;
      esac
      continue
    fi
    case "$c" in
      "'" | '"') quote=$c in_word=1 ;;
      \\) word+=${s:i:1} in_word=1 && i=$((i + 1)) ;;
      ' ' | $'\t' | '' | ';' | '&' | '|' | '(' | ')' | $'\n')
        if [ -n "$in_word" ]; then
          words+=("$word")
          [ "$word" = CHECKS=full ] && override=1
        fi
        word='' in_word=''
        if [ "$c" != ' ' ] && [ "$c" != $'\t' ]; then
          [ ${#words[@]} -gt 0 ] && check_segment "${words[@]}"
          words=()
        fi
        # Skip the bodies of the heredocs this line opened.
        if [ "$c" = $'\n' ] && [ ${#heredocs[@]} -gt 0 ]; then
          for delim in "${heredocs[@]}"; do
            strip=${delim%%:*} delim=${delim#*:}
            while [ $i -lt "$n" ]; do
              line=${s:i}
              line=${line%%$'\n'*}
              i=$((i + ${#line} + 1))
              [ -n "$strip" ] && line=${line#"${line%%[!$'\t']*}"}
              [ "$line" = "$delim" ] && break
            done
          done
          heredocs=()
        fi
        ;;
      '<')
        if [ "${s:i:2}" = '<<' ]; then
          word+='<<<' in_word=1
          i=$((i + 2))
        elif [ "${s:i:1}" = '<' ]; then
          i=$((i + 1))
          strip=
          [ "${s:i:1}" = - ] && strip=1 && i=$((i + 1))
          while [ "${s:i:1}" = ' ' ]; do i=$((i + 1)); done
          delim=${s:i}
          delim=${delim%%[ $'\t\n;&|()<>']*}
          i=$((i + ${#delim}))
          delim=${delim//[\'\"\\]/}
          heredocs+=("$strip:$delim")
        else
          word+=$c in_word=1
        fi
        ;;
      *) word+=$c in_word=1 ;;
    esac
  done
  return 0
}

scan "$command"

if [ ${#reasons[@]} -eq 0 ] || [ -n "$override" ]; then
  exit 0
fi

reason=$(printf '%s\n' "${reasons[@]}" | awk '!seen[$0]++')
jq -n --arg reason "$reason" \
  '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $reason}}'

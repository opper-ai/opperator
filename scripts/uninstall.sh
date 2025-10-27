#!/usr/bin/env bash

set -euo pipefail

APP="opperator"
INSTALL_DIR="${OPPERATOR_INSTALL_DIR:-${OPERATOR_INSTALL_DIR:-$HOME/.opperator/bin}}"
VERSION_FILE="$INSTALL_DIR/${APP}.version"
ALIAS_OVERRIDE="${ALIAS_DIR:-}"

ORANGE='\033[38;2;255;140;0m'
RESET='\033[0m'

print_message() {
  local level=$1
  local message=$2

  case "$level" in
    error)
      if [[ -n "${NO_COLOR:-}" ]]; then
        printf '%s\n' "$message"
      else
        printf '\033[0;31m%s\033[0m\n' "$message"
      fi
      ;;
    warning)
      if [[ -n "${NO_COLOR:-}" ]]; then
        printf '%s\n' "$message"
      else
        printf '\033[1;33m%s\033[0m\n' "$message"
      fi
      ;;
    success)
      if [[ -n "${NO_COLOR:-}" ]]; then
        printf '%s\n' "$message"
      else
        printf '\033[0;32m%s\033[0m\n' "$message"
      fi
      ;;
    *)
      printf '%s\n' "$message"
      ;;
  esac
}

colorize() {
  local word="$*"
  if [[ -n "${NO_COLOR:-}" ]]; then
    printf '%s' "$word"
    return
  fi
  printf '%b%s%b' "$ORANGE" "$word" "$RESET"
}

expand_path() {
  local path="$1"
  if [[ -z "$path" ]]; then
    printf '%s\n' "$path"
    return
  fi
  if [[ "$path" == ~* ]]; then
    local home="${HOME:-}"
    if [[ -n "$home" ]]; then
      printf '%s%s\n' "$home" "${path:1}"
      return
    fi
  fi
  printf '%s\n' "$path"
}

normalize_path() {
  local expanded
  expanded=$(expand_path "$1")
  if [[ -d "$expanded" ]]; then
    if dir_norm=$(cd "$expanded" 2>/dev/null && pwd); then
      printf '%s\n' "$dir_norm"
      return
    fi
  fi
  printf '%s\n' "$expanded"
}

alias_candidates() {
  if [[ -n "$ALIAS_OVERRIDE" ]]; then
    normalize_path "$ALIAS_OVERRIDE"
    return
  fi

  local home="${HOME:-}"
  if [[ -n "$home" ]]; then
    normalize_path "$home/.local/bin"
    normalize_path "$home/bin"
  fi
  normalize_path "/usr/local/bin"
}

remove_symlink() {
  local link="$1"
  local removed=1
  if [[ -L "$link" ]]; then
    local target
    target=$(readlink "$link" || true)
    if [[ "$target" == "$INSTALL_DIR/$APP" ]]; then
      rm -f "$link"
      removed=0
    fi
  fi
  return $removed
}

removed_any=1

if [[ -z "${ASSUME_YES:-}" ]]; then
  read -r -p "Remove Opperator and its command links? [y/N] " confirm
  case "$confirm" in
    [yY][eE][sS]|[yY])
      ;;
    *)
      print_message warning "Uninstall cancelled."
      exit 0
      ;;
  esac
fi

remove_configs="no"
if [[ -z "${ASSUME_YES:-}" ]]; then
  print_message info ""
  print_message info "Would you also like to remove Opperator configuration and agents?"
  print_message warning "Configuration and agent data live under ~/.config/opperator/"
  print_message info ""
  read -r -p "Delete Opperator configuration and agents? [y/N] " config_prompt
  case "$config_prompt" in
    [yY][eE][sS]|[yY])
      print_message error "WARNING: This action cannot be reversed."
      print_message info ""
      read -r -p "Type 'delete' to confirm, or press Enter to skip: " final_confirm
      if [[ "$final_confirm" == "delete" ]]; then
        print_message info ""
        remove_configs="yes"
      else
        print_message warning "Skipping user configuration removal."
        print_message info ""
      fi
      ;;
    *)
      print_message warning "Skipping configuration removal."
      print_message info ""
      ;;
  esac
fi

while IFS= read -r candidate; do
  [[ -z "$candidate" ]] && continue
  for name in "$APP" "op"; do
    path="$candidate/$name"
    if remove_symlink "$path"; then
      print_message info "Removed $(colorize "$name") link from $candidate"
      removed_any=0
    fi
  done
done < <(alias_candidates | uniq)

binary_path="$INSTALL_DIR/$APP"
if [[ -f "$binary_path" ]]; then
  rm -f "$binary_path"
  print_message info "Removed binary $(colorize "$APP") from $INSTALL_DIR"
  removed_any=0
fi

if [[ -f "$VERSION_FILE" ]]; then
  rm -f "$VERSION_FILE"
fi

if [[ -d "$INSTALL_DIR" ]]; then
  if rmdir "$INSTALL_DIR" 2>/dev/null; then
    print_message info "Removed empty install directory $INSTALL_DIR"
  fi
fi

if [[ $removed_any -ne 0 ]]; then
  print_message info "No installation artifacts were found."
fi

if [[ "$remove_configs" == "yes" ]]; then
  print_message info ""
  print_message warning "Configuration removal is not implemented yet. Please delete ~/.config/opperator/ manually if desired."
else
  print_message info ""
  config_path="${HOME:-}/.config/opperator/"
  print_message info "User configuration and agents were not removed; you can still access them under $(colorize "$config_path")"
fi

print_message info ""
print_message info "ℹ Run $(colorize "curl -fsSL https://opper.ai/opperator-install | bash") to reinstall."

print_message info ""
print_message success "✔︎ Uninstall complete."

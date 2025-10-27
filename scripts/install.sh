#!/usr/bin/env bash

set -euo pipefail

APP="opperator"
REPO="opper-ai/opperator"
INSTALL_DIR="${OPPERATOR_INSTALL_DIR:-${OPERATOR_INSTALL_DIR:-$HOME/.opperator/bin}}"
VERSION_FILE="$INSTALL_DIR/${APP}.version"
tmp_dir=""
alias_dir=""
alias_dir_override="${ALIAS_DIR:-}"

ORANGE='\033[38;2;247;192;175m'
RESET='\033[0m'

requested_version="${VERSION:-}"

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

colorize_app() {
  if [[ -n "${NO_COLOR:-}" ]]; then
    printf '%s' "$APP"
    return
  fi

  printf '%b%s%b' "$ORANGE" "$APP" "$RESET"
}

colorize_word() {
  local word=$1
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
  local raw="$1"
  local expanded
  expanded=$(expand_path "$raw")
  if [[ -d "$expanded" ]]; then
    if dir_norm=$(cd "$expanded" 2>/dev/null && pwd); then
      printf '%s\n' "$dir_norm"
      return
    fi
  fi
  printf '%s\n' "$expanded"
}

path_contains() {
  local dir_norm
  dir_norm=$(normalize_path "$1")
  local entry
  IFS=':' read -r -a entries <<<"${PATH:-}"
  for entry in "${entries[@]}"; do
    [[ -z "$entry" ]] && continue
    if [[ "$dir_norm" == "$(normalize_path "$entry")" ]]; then
      return 0
    fi
  done
  return 1
}

default_alias_dir() {
  local home="${HOME:-}"
  local candidates=()

  candidates+=("$INSTALL_DIR")
  if [[ -n "$home" ]]; then
    candidates+=("$home/.opperator/bin" "$home/.local/bin" "$home/bin")
  fi

  for candidate in "${candidates[@]}"; do
    local expanded
    expanded=$(expand_path "$candidate")
    if [[ -n "$expanded" ]]; then
      printf '%s\n' "$expanded"
      return
    fi
  done

  local path_entries=()
  IFS=':' read -r -a path_entries <<<"${PATH:-}"
  for entry in "${path_entries[@]}"; do
    [[ -z "$entry" ]] && continue
    local normalized
    normalized=$(normalize_path "$entry")
    if [[ -n "$normalized" && -w "$normalized" ]]; then
      printf '%s\n' "$normalized"
      return
    fi
  done

  printf '%s\n' "$(normalize_path "$INSTALL_DIR")"
}

select_alias_dir() {
  if [[ -n "$alias_dir_override" ]]; then
    printf '%s\n' "$(expand_path "$alias_dir_override")"
  else
    default_alias_dir
  fi
}

ensure_symlink() {
  local link_path="$1"
  local target="$2"

  if [[ -L "$link_path" ]]; then
    local existing
    existing=$(readlink "$link_path" || true)
    if [[ "$existing" == "$target" ]]; then
      return 0
    fi
  fi

  if [[ -e "$link_path" ]]; then
    rm -f "$link_path" || return 1
  fi

  ln -s "$target" "$link_path"
}

setup_aliases() {
  local dir
  dir=$(select_alias_dir)
  dir=$(expand_path "$dir")

  if [[ -z "$dir" ]]; then
    print_message warning "Alias directory is empty; skipping command links"
    alias_dir=""
    return 1
  fi

  if ! mkdir -p "$dir" 2>/dev/null; then
    print_message warning "Unable to create alias directory $dir"
    alias_dir=""
    return 1
  fi

  local target_path="$INSTALL_DIR/$APP"
  local failures=0
  for link_name in "$APP" "op"; do
    local link_path="$dir/$link_name"
    if [[ "$link_path" == "$target_path" ]]; then
      continue
    fi
    if ! ensure_symlink "$link_path" "$target_path"; then
      print_message warning "Failed to create symlink $link_path"
      failures=1
    fi
  done

  if [[ $failures -eq 0 ]]; then
    alias_dir="$dir"
    print_message info "✔︎ Linked Opperator into $dir"
    return 0
  fi

  alias_dir=""
  return 1
}

fail() {
  print_message error "$1"
  exit 1
}

require_cmd() {
  local cmd=$1
  command -v "$cmd" >/dev/null 2>&1 || fail "Missing required command: $cmd"
}

require_cmd curl
require_cmd tar

trap '[[ -n "${tmp_dir:-}" ]] && rm -rf "$tmp_dir"' EXIT

os_name=$(uname -s)
case "$os_name" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) fail "Unsupported operating system: $os_name" ;;
esac

arch_name=$(uname -m)
case "$arch_name" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) fail "Unsupported architecture: $arch_name" ;;
esac

archive_ext="tar.gz"

resolve_version() {
  local version="$requested_version"
  local release_data=""

  if [[ -n "$version" ]]; then
    if ! release_data=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/tags/$version" 2>/dev/null); then
      fail "Release $version not found for $REPO"
    fi
  else
    print_message info "Resolving latest release from GitHub"
    if ! release_data=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null); then
      print_message error "No stable release found. Please specify a version with VERSION=<version>"
      print_message info "Example: curl -fsSL http://opper.ai/opperator-install | VERSION=v0.1.0-alpha bash"
      return 1
    fi
  fi

  local tag
  tag=$(printf '%s\n' "$release_data" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  if [[ -z "$tag" ]]; then
    fail "Unable to determine release tag"
  fi

  specific_version="$tag"
  local asset_pattern="$APP-${specific_version}-${os}-${arch}.${archive_ext}"
  download_url=$(printf '%s\n' "$release_data" | grep '"browser_download_url"' | grep "$asset_pattern" | head -n1 | sed -E 's/.*"browser_download_url": *"([^"]+)".*/\1/')

  if [[ -z "$download_url" ]]; then
    fail "No release asset found for $asset_pattern"
  fi
}

check_installed_version() {
  if [[ -f "$VERSION_FILE" ]]; then
    local installed_version
    installed_version=$(<"$VERSION_FILE")
    if [[ "$installed_version" == "$specific_version" ]]; then
      print_message warning "Opperator ${specific_version} already installed"
      print_message info ""

      if [[ -z "${ASSUME_YES:-}" ]]; then
        read -r -p "Reinstall anyway? [y/N] " answer
      else
        answer="y"
      fi

      case "$answer" in
        [yY][eE][sS]|[yY])
          print_message info "Reinstalling Opperator ${specific_version}" 
          return
          ;;
        *)
          print_message warning "Leaving existing installation unchanged"
          exit 0
          ;;
      esac
    fi

    print_message warning "Existing installation detected: ${installed_version} (will be replaced)"
  elif command -v "$APP" >/dev/null 2>&1; then
    local existing_path
    existing_path=$(command -v "$APP")
    print_message warning "Opperator already present at ${existing_path}; version unknown and will be replaced"
  fi
}

install_release() {
  print_message info "Downloading Opperator ${specific_version} for ${os}/${arch}"

  mkdir -p "$INSTALL_DIR"
  tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t opperator-install)

  local archive_name="$APP-${specific_version}-${os}-${arch}.${archive_ext}"

  if ! curl -fL "${download_url}" -o "$tmp_dir/$archive_name"; then
    fail "Failed to download release artifact"
  fi

  (cd "$tmp_dir" && tar -xzf "$archive_name")

  local extracted_binary="$tmp_dir/$APP-${specific_version}-${os}-${arch}"
  if [[ ! -f "$extracted_binary" ]]; then
    extracted_binary=$(find "$tmp_dir" -maxdepth 2 -type f \( -name "$APP" -o -name "$APP.exe" \) -print -quit || true)
  fi

  if [[ -z "$extracted_binary" || ! -f "$extracted_binary" ]]; then
    fail "Expected binary not found in release archive"
  fi

  local target_path="$INSTALL_DIR/$APP"
  mv "$extracted_binary" "$target_path"
  chmod +x "$target_path"
  printf '%s\n' "$specific_version" > "$VERSION_FILE"

  print_message info ""
  print_message info "✔︎ Installed Opperator to ${target_path}"
}

add_to_path() {
  local config_file=$1
  local command=$2

  if grep -Fqx "$command" "$config_file" 2>/dev/null; then
    print_message warning "PATH entry already present in $config_file"
    return
  fi

  if [[ -w "$config_file" ]]; then
    {
      printf '\n# %s\n' "$APP"
      printf '%s\n' "$command"
    } >> "$config_file"
    print_message info "✔︎ Added 'opperator' to PATH via $config_file"
  else
    print_message warning "Cannot modify $config_file automatically. Add the following manually:"
    print_message info "  $command"
  fi
}

ensure_path_configuration() {
  local bin_dir="$1"

  if [[ ":$PATH:" == *":$bin_dir:"* ]]; then
    return
  fi

  local xdg_config_home
  xdg_config_home=${XDG_CONFIG_HOME:-$HOME/.config}
  local home="${HOME:-}"

  local current_shell
  current_shell=$(basename "${SHELL:-}")

  local config_files=""
  case "$current_shell" in
    fish)
      config_files="$HOME/.config/fish/config.fish"
      path_command="fish_add_path $bin_dir"
      ;;
    zsh)
      config_files="$HOME/.zshrc $HOME/.zshenv $xdg_config_home/zsh/.zshrc $xdg_config_home/zsh/.zshenv"
      path_command="export PATH=$bin_dir:\$PATH"
      ;;
    bash)
      config_files="$HOME/.bashrc $HOME/.bash_profile $HOME/.profile $xdg_config_home/bash/.bashrc $xdg_config_home/bash/.bash_profile"
      path_command="export PATH=$bin_dir:\$PATH"
      ;;
    ash|sh)
      config_files="$HOME/.ashrc $HOME/.profile /etc/profile"
      path_command="export PATH=$bin_dir:\$PATH"
      ;;
    *)
      config_files="$HOME/.profile $HOME/.bash_profile $HOME/.bashrc"
      path_command="export PATH=$bin_dir:\$PATH"
      ;;
  esac

  local selected_config=""
  local create_candidate=""

  for file in $config_files; do
    if [[ -f "$file" ]]; then
      selected_config="$file"
      break
    fi

    if [[ -z "$create_candidate" ]]; then
      if [[ -n "$home" && "$file" == "$home"/* ]]; then
        create_candidate="$file"
      elif [[ -n "$xdg_config_home" && "$file" == "$xdg_config_home"/* ]]; then
        create_candidate="$file"
      fi
    fi
  done

  if [[ -z "$selected_config" && -n "$create_candidate" ]]; then
    local create_dir
    create_dir=$(dirname "$create_candidate")
    if mkdir -p "$create_dir" 2>/dev/null; then
      if [[ ! -f "$create_candidate" ]]; then
        if touch "$create_candidate" 2>/dev/null; then
          selected_config="$create_candidate"
          print_message info "Created shell config at $create_candidate"
        fi
      else
        selected_config="$create_candidate"
      fi
    fi
  fi

  if [[ -n "$selected_config" ]]; then
    add_to_path "$selected_config" "$path_command"
  else
    print_message warning "Could not locate a writable shell config file to update PATH"
    print_message warning "Add the following to your shell init script: $path_command"
  fi
}

resolve_version
check_installed_version
install_release

setup_aliases || true

path_bin_dir=$(expand_path "${alias_dir:-$INSTALL_DIR}")
colored_app=$(colorize_app)
colored_op=$(colorize_word "op")

ensure_path_configuration "$path_bin_dir"

if [[ ":$PATH:" != *":$path_bin_dir:"* ]]; then
  export PATH="$path_bin_dir:$PATH"
  print_message warning "Temporarily added opperator commands to PATH for this session"
fi

if [[ -n "$alias_dir" ]]; then
  if path_contains "$alias_dir"; then
    :
  else
    print_message warning "Add $alias_dir to your PATH so '${colored_op}' and '${colored_app}' are available in new shells."
  fi
else
  print_message warning "Symlinks not created; use '$INSTALL_DIR/$APP' directly or add it to PATH manually."
fi

print_message info ""
print_message success "🚀 Installation complete!"
print_message info ""
print_message info "Run '${colored_op}' or '${colored_app}' to launch."

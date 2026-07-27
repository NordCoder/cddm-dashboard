#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
install_dir="${CDDM_INSTALL_DIR:-$HOME/.local/bin}"
target="$install_dir/cddm"
revision="$(git -C "$repo_root" rev-parse HEAD)"

tmp="$(mktemp "${TMPDIR:-/tmp}/cddm.XXXXXX")"
trap 'rm -f "$tmp"' EXIT

mkdir -p "$install_dir"
(
  cd "$repo_root/backend"
  go build -trimpath -ldflags "-X main.buildRevision=$revision" -o "$tmp" ./cmd/cddm-change
)
install -m 0755 "$tmp" "$target"

built_revision="$("$target" __build-revision)"
if [[ "$built_revision" != "$revision" ]]; then
  echo "Installed cddm revision mismatch: built=$built_revision expected=$revision" >&2
  exit 1
fi

echo "Installed CDDM CLI: $target"
echo "Revision: $revision"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    echo
    echo "Add this directory to PATH:"
    echo "  export PATH=\"$install_dir:\$PATH\""
    ;;
esac

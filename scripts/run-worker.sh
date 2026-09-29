#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s <0.27.0|0.44.0> <artifact-dir> -- <command> [args...]\n' "$0" >&2
  exit 2
}

[[ $# -ge 4 ]] || usage
jj_version=$1
artifact_dir=$2
shift 2
[[ $1 == -- ]] || usage
shift
[[ $# -gt 0 ]] || usage

case "$jj_version" in
  0.27.0 | 0.44.0) ;;
  *) usage ;;
esac

source_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
mkdir -p "$artifact_dir"
artifact_dir=$(cd "$artifact_dir" && pwd -P)

if [[ $artifact_dir == / ]]; then
  printf 'artifact directory must not be the filesystem root\n' >&2
  exit 1
fi

case "$source_dir/" in
  "$artifact_dir/"*)
    printf 'artifact directory must not be the source directory or one of its ancestors\n' >&2
    exit 1
    ;;
esac

if [[ $(podman info --format '{{.Host.Security.Rootless}}') != true ]]; then
  printf 'jj-stacked validation workers require rootless Podman\n' >&2
  exit 1
fi

image_archive=$(nix build \
  "path:$source_dir#workerImage" \
  --no-link \
  --print-out-paths)

# Loading and resolving the immutable image ID are serialized because the archive
# carries a shared human-readable tag. Workers run by ID after releasing the lock.
if [[ -n ${XDG_RUNTIME_DIR:-} ]]; then
  runtime_parent=$XDG_RUNTIME_DIR
  if [[ ! -d $runtime_parent || -L $runtime_parent || ! -O $runtime_parent ]]; then
    printf 'XDG_RUNTIME_DIR must be an owned, non-symlink directory: %s\n' "$runtime_parent" >&2
    exit 1
  fi
else
  runtime_parent=/tmp
fi
runtime_dir="$runtime_parent/jj-stacked-worker-$(id -u)"
if [[ -e $runtime_dir || -L $runtime_dir ]]; then
  if [[ ! -d $runtime_dir || -L $runtime_dir || ! -O $runtime_dir ]]; then
    printf 'worker runtime path must be an owned, non-symlink directory: %s\n' "$runtime_dir" >&2
    exit 1
  fi
else
  mkdir "$runtime_dir"
fi
if [[ ! -d $runtime_dir || -L $runtime_dir || ! -O $runtime_dir ]]; then
  printf 'worker runtime path must be an owned, non-symlink directory: %s\n' "$runtime_dir" >&2
  exit 1
fi
chmod 700 "$runtime_dir"
exec 9>"$runtime_dir/jj-stacked-worker-image.lock"
flock 9
image_stamp="$runtime_dir/$(basename "$image_archive").id"
image_id=
if [[ -r $image_stamp ]]; then
  image_id=$(<"$image_stamp")
  if ! podman image exists "$image_id"; then
    image_id=
  fi
fi
if [[ -z $image_id ]]; then
  podman load --quiet --input "$image_archive" >/dev/null
  image_id=$(podman image inspect \
    localhost/jj-stacked-validation-worker:go1.26.6 \
    --format '{{.Id}}')
  printf '%s\n' "$image_id" >"$image_stamp.tmp"
  chmod 600 "$image_stamp.tmp"
  mv "$image_stamp.tmp" "$image_stamp"
fi
flock -u 9

uid=$(id -u)
gid=$(id -g)
exec podman run --rm \
  --network=none \
  --read-only \
  --userns=keep-id \
  --user="$uid:$gid" \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --pids-limit=2048 \
  --tmpfs /tmp:rw,nosuid,nodev,size=4g \
  --mount "type=bind,src=$source_dir,dst=/src,readonly" \
  --mount "type=bind,src=$artifact_dir,dst=/artifacts,rw" \
  --env "JJ_VERSION=$jj_version" \
  "$image_id" \
  "$@"

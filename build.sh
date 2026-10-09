#!/usr/bin/env bash
# ARTIFEX cross-platform release builder.
#
# The default mode builds one target and embeds the already-exported frontend.
# `./build.sh --release` builds and packages all supported desktop/server targets.
#
# Environment variables:
#   ARTIFEX_TARGET_OS=linux             One target OS in single-target mode.
#   ARTIFEX_TARGET_ARCH=amd64           One target arch in single-target mode.
#   ARTIFEX_TARGETS=linux/amd64,...     Comma-separated targets for multi-target mode.
#   ARTIFEX_BUILD_VERSION=v0.3.3        Version embedded in the binary and archive name.
#   ARTIFEX_OUTPUT=/path/to/artifex       Explicit binary path in single-target mode.
#   ARTIFEX_OUTPUT_DIR=dist             Directory for default binary paths.
#   ARTIFEX_PACKAGE=1                   Create a zip archive for each target.
#   ARTIFEX_PACKAGE_DIR=dist            Directory for release archives.
#   ARTIFEX_COMPRESS=off                UPX mode: off, auto, or required.
#   ARTIFEX_UPX_ARGS="--best --lzma"    Arguments passed to UPX.
#   ARTIFEX_SKIP_FRONTEND=1             Reuse server/webui/dist (for CI artifact builds).
#   ARTIFEX_SKIP_NPM_CI=1               Skip npm ci while rebuilding the frontend.
#   ARTIFEX_GOSUMDB=sum.golang.org      Go checksum database.
set -euo pipefail
LANG_SEL="${ARTIFEX_LANGUAGE:-${ARTIFEX_LANGUAGE:-en}}"
case "$LANG_SEL" in ko|ko_*|ko-*|KO) LANG_SEL=ko ;; *) LANG_SEL=en ;; esac
msg(){ if [ "$LANG_SEL" = ko ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }


cd "$(cd "$(dirname "$0")" && pwd)"

info() { printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok() { printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[!]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  if [ "$LANG_SEL" = ko ]; then
    cat <<'EOF'
사용법: ./build.sh [--target OS/ARCH | --release] [--upx | --no-compress]
기본값: 현재 시스템/아키텍처를 빌드합니다.
--release: Linux 및 macOS amd64/arm64, Windows amd64를 빌드하고 zip으로 묶습니다.
--target OS/ARCH: 대상 지정 (예: windows/amd64).
--upx: UPX 압축 필수 (일부 Linux 환경과 호환되지 않을 수 있음).
--no-compress: UPX 없이 Go linker 최적화 및 zip 압축만 사용합니다.
--help: 도움말 표시.
ARTIFEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release로 대상 목록을 지정합니다.
EOF
  else
    cat <<'EOF'
Usage: ./build.sh [--target OS/ARCH | --release] [--upx | --no-compress]
Default: build the current system and architecture.
--release: build and zip Linux/macOS amd64/arm64 and Windows amd64.
--target OS/ARCH: select a target, for example windows/amd64.
--upx: require UPX compression (may reduce compatibility on some Linux systems).
--no-compress: use Go linker stripping and zip compression without UPX.
--help: show help.
Override targets with ARTIFEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release.
EOF
  fi
}

RELEASE_TARGETS_DEFAULT="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64"
ARTIFEX_RELEASE="${ARTIFEX_RELEASE:-0}"
ARTIFEX_COMPRESS="${ARTIFEX_COMPRESS:-off}"
ARTIFEX_PACKAGE="${ARTIFEX_PACKAGE:-0}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --release)
      ARTIFEX_RELEASE=1
      ARTIFEX_PACKAGE=1
      shift
      ;;
    --target)
      [ "$#" -ge 2 ] || die "$(msg "--target requires OS/ARCH" "--target에 OS/ARCH가 필요합니다")"
      target_arg="$2"
      case "$target_arg" in
        */*)
          ARTIFEX_TARGET_OS="${target_arg%%/*}"
          ARTIFEX_TARGET_ARCH="${target_arg##*/}"
          ARTIFEX_TARGETS="$target_arg"
          ;;
        *) die "$(msg "Target must be OS/ARCH, e.g. linux/amd64" "대상은 OS/ARCH 형식이어야 합니다 (예: linux/amd64)")" ;;
      esac
      shift 2
      ;;
    --no-compress)
      ARTIFEX_COMPRESS=0
      shift
      ;;
    --upx)
      ARTIFEX_COMPRESS=required
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *) die "$(msg "Unknown argument: $1 (see --help)" "알 수 없는 인수: $1 (--help 참고)")" ;;
  esac
done

command -v go >/dev/null 2>&1 || die "$(msg "Go not found (Go 1.26 or later required)" "Go를 찾을 수 없습니다 (Go 1.26 이상 필요)")"

ARTIFEX_GOSUMDB="${ARTIFEX_GOSUMDB:-sum.golang.org}"
if [ -z "${ARTIFEX_BUILD_VERSION:-}" ]; then
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    ARTIFEX_BUILD_VERSION="$(git describe --tags --always --dirty)"
  else
    ARTIFEX_BUILD_VERSION="dev"
  fi
fi
# Release tags are commonly passed as v0.3.3; keep the binary version consistent.
ARTIFEX_BUILD_VERSION="${ARTIFEX_BUILD_VERSION#v}"
ARTIFEX_OUTPUT_DIR="${ARTIFEX_OUTPUT_DIR:-dist}"
ARTIFEX_PACKAGE_DIR="${ARTIFEX_PACKAGE_DIR:-$ARTIFEX_OUTPUT_DIR}"
ARTIFEX_UPX_ARGS="${ARTIFEX_UPX_ARGS:---best --lzma}"

if [ "${ARTIFEX_RELEASE}" = "1" ]; then
  ARTIFEX_TARGETS="${ARTIFEX_TARGETS:-$RELEASE_TARGETS_DEFAULT}"
else
  ARTIFEX_TARGET_OS="${ARTIFEX_TARGET_OS:-$(GOSUMDB="$ARTIFEX_GOSUMDB" go env GOOS)}"
  ARTIFEX_TARGET_ARCH="${ARTIFEX_TARGET_ARCH:-$(GOSUMDB="$ARTIFEX_GOSUMDB" go env GOARCH)}"
  ARTIFEX_TARGETS="${ARTIFEX_TARGETS:-${ARTIFEX_TARGET_OS}/${ARTIFEX_TARGET_ARCH}}"
fi

if [ "${ARTIFEX_SKIP_FRONTEND:-0}" = "1" ]; then
  [ -d server/webui/dist ] || die "$(msg "ARTIFEX_SKIP_FRONTEND=1 but server/webui/dist is missing" "ARTIFEX_SKIP_FRONTEND=1이지만 server/webui/dist가 없습니다")"
else
  command -v npm >/dev/null 2>&1 || die "$(msg "npm not found (Node.js/npm required for frontend export)" "npm을 찾을 수 없습니다 (프런트엔드 빌드에 Node.js/npm 필요)")"
  command -v rsync >/dev/null 2>&1 || die "$(msg "rsync not found" "rsync를 찾을 수 없습니다")"
  info "$(msg "Building frontend static assets" "프런트엔드 정적 파일 빌드 중")"
  if [ "${ARTIFEX_SKIP_NPM_CI:-0}" != "1" ]; then
    (cd web && npm ci)
  fi
  (cd web && npm run build:static)
  info "$(msg "Syncing frontend to server/webui/dist" "프런트엔드를 server/webui/dist에 동기화 중")"
  mkdir -p server/webui/dist
  rsync -a --delete web/out/ server/webui/dist/
fi

compress_binary() {
  binary="$1"
  goos="$2"
  case "$ARTIFEX_COMPRESS" in
    0|off|false|none)
      info "$(msg "Skipping UPX: $binary" "UPX 건너뛰기: $binary")"
      return 0
      ;;
    auto|required|true|1) ;;
    *) die "$(msg "ARTIFEX_COMPRESS must be off, auto, or required" "ARTIFEX_COMPRESS는 off, auto 또는 required여야 합니다")" ;;
  esac

  if ! command -v upx >/dev/null 2>&1; then
    if [ "$ARTIFEX_COMPRESS" = "required" ]; then
      die "$(msg "ARTIFEX_COMPRESS=required but upx was not found" "ARTIFEX_COMPRESS=required이지만 upx를 찾을 수 없습니다")"
    fi
    warn "$(msg "upx not found; retaining linker output: $binary" "upx를 찾을 수 없어 linker 결과 유지: $binary")"
    return 0
  fi

  before=$(wc -c < "$binary" | tr -d ' ')
  upx_args="$ARTIFEX_UPX_ARGS"
  [ "$goos" = "darwin" ] && upx_args="$upx_args --force-macos"
  # shellcheck disable=SC2086
  if ! upx $upx_args -- "$binary"; then
    if [ "$ARTIFEX_COMPRESS" = "required" ]; then
      die "$(msg "UPX compression failed: $binary" "UPX 압축 실패: $binary")"
    fi
    warn "$(msg "UPX does not support this format; retaining binary: $binary" "UPX가 이 형식을 지원하지 않아 바이너리 유지: $binary")"
    return 0
  fi
  after=$(wc -c < "$binary" | tr -d ' ')
  ok "$(msg "UPX complete: $binary (${before} -> ${after} bytes)" "UPX 완료: $binary (${before} -> ${after} 바이트)")"
}

package_binary() {
  binary="$1"
  goos="$2"
  goarch="$3"
  package_name="artifex-${ARTIFEX_BUILD_VERSION}-${goos}-${goarch}"
  package_root="${ARTIFEX_PACKAGE_DIR}/${package_name}"
  archive="${ARTIFEX_PACKAGE_DIR}/${package_name}.zip"

  command -v zip >/dev/null 2>&1 || die "$(msg "Packaging requires zip" "패키징에 zip이 필요합니다")"
  rm -rf "$package_root" "$archive"
  mkdir -p "$package_root"
  cp "$binary" "$package_root/"
  # Package the platform supervisor so updates can restart the process.
  if [ "$goos" = "windows" ]; then
    cp start.bat "$package_root/"
  else
    cp start.sh "$package_root/"
    chmod +x "$package_root/start.sh"
  fi
  cp -R skills "$package_root/"
  cp config.example.json "$package_root/"
  cp README.md LICENSE CHANGELOG.md "$package_root/"
  if [ -d docs ]; then cp -R docs "$package_root/"; fi
  # Ship the adapter sources without local dependencies, credentials or runtime state.
  mkdir -p "$package_root/adapters/agent"
  cp adapters/agent/package.json adapters/agent/package-lock.json adapters/agent/README.md adapters/agent/pi-extension.js "$package_root/adapters/agent/"
  cp -R adapters/agent/src adapters/agent/test "$package_root/adapters/agent/"
  mkdir -p "$package_root/log"
  cp log/changelog-*.md "$package_root/log/"
  (cd "$ARTIFEX_PACKAGE_DIR" && zip -q -r -9 "$(basename "$archive")" "$(basename "$package_root")")
  rm -rf "$package_root"
  ok "$(msg "Release archive: $archive" "릴리스 압축 파일: $archive")"
}

build_target() {
  target="$1"
  case "$target" in
    */*) ;;
    *) die "$(msg "Invalid target: $target (expected OS/ARCH)" "잘못된 대상: $target (OS/ARCH 필요)")" ;;
  esac
  goos="${target%%/*}"
  goarch="${target##*/}"
  case "$goos" in
    linux|darwin|windows) ;;
    *) die "$(msg "Unsupported OS: $goos (use linux, darwin, windows)" "지원하지 않는 OS: $goos (linux, darwin, windows 지원)")" ;;
  esac

  binary_name="artifex"
  [ "$goos" = "windows" ] && binary_name="artifex.exe"
  if [ -n "${ARTIFEX_OUTPUT:-}" ] && [ "$ARTIFEX_RELEASE" != "1" ]; then
    output="$ARTIFEX_OUTPUT"
  else
    output="${ARTIFEX_OUTPUT_DIR}/artifex-${goos}-${goarch}/${binary_name}"
  fi
  mkdir -p "$(dirname "$output")"

  info "$(msg "Building ${goos}/${goarch}, version ${ARTIFEX_BUILD_VERSION}" "${goos}/${goarch} 빌드 중, 버전 ${ARTIFEX_BUILD_VERSION}")"
  GOSUMDB="$ARTIFEX_GOSUMDB" \
  CGO_ENABLED=0 \
  GOOS="$goos" \
  GOARCH="$goarch" \
  go build \
    -tags embedui \
    -trimpath \
    -ldflags "-s -w -buildid= -X main.version=${ARTIFEX_BUILD_VERSION}" \
    -o "$output" \
    ./cmd/artifex

  compress_binary "$output" "$goos"
  if command -v file >/dev/null 2>&1; then file "$output"; fi
  if [ "$ARTIFEX_PACKAGE" = "1" ]; then package_binary "$output" "$goos" "$goarch"; fi
  ok "$(msg "Build complete: $output" "빌드 완료: $output")"
}

write_checksums() {
  [ "$ARTIFEX_PACKAGE" = "1" ] || return 0
  checksum_file="$ARTIFEX_PACKAGE_DIR/SHA256SUMS"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$ARTIFEX_PACKAGE_DIR" && for archive in *.zip; do sha256sum "$archive"; done > "$(basename "$checksum_file")")
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$ARTIFEX_PACKAGE_DIR" && for archive in *.zip; do shasum -a 256 "$archive"; done > "$(basename "$checksum_file")")
  else
    warn "$(msg "sha256sum and shasum unavailable; skipping SHA256SUMS" "sha256sum과 shasum이 없어 SHA256SUMS 건너뛰기")"
    return 0
  fi
  ok "$(msg "Checksum file: $checksum_file" "체크섬 파일: $checksum_file")"
}

mkdir -p "$ARTIFEX_OUTPUT_DIR"
if [ "$ARTIFEX_PACKAGE" = "1" ]; then mkdir -p "$ARTIFEX_PACKAGE_DIR"; fi

old_ifs="$IFS"
IFS=','
read -r -a targets <<< "$ARTIFEX_TARGETS"
IFS="$old_ifs"
[ "${#targets[@]}" -gt 0 ] || die "$(msg "ARTIFEX_TARGETS cannot be empty" "ARTIFEX_TARGETS는 비어 있을 수 없습니다")"
for target in "${targets[@]}"; do
  target="${target//[[:space:]]/}"
  [ -n "$target" ] || continue
  build_target "$target"
done

if [ "$ARTIFEX_PACKAGE" = "1" ]; then
  write_checksums
  info "$(msg "Release archives generated in: $ARTIFEX_PACKAGE_DIR" "릴리스 압축 파일 생성 위치: $ARTIFEX_PACKAGE_DIR")"
fi

#!/usr/bin/env bash
# ARTEX cross-platform release builder.
#
# The default mode builds one target and embeds the already-exported frontend.
# `./build.sh --release` builds and packages all supported desktop/server targets.
#
# Environment variables:
#   ARTEX_TARGET_OS=linux             One target OS in single-target mode.
#   ARTEX_TARGET_ARCH=amd64           One target arch in single-target mode.
#   ARTEX_TARGETS=linux/amd64,...     Comma-separated targets for multi-target mode.
#   ARTEX_BUILD_VERSION=v0.3.3        Version embedded in the binary and archive name.
#   ARTEX_OUTPUT=/path/to/artex       Explicit binary path in single-target mode.
#   ARTEX_OUTPUT_DIR=dist             Directory for default binary paths.
#   ARTEX_PACKAGE=1                   Create a zip archive for each target.
#   ARTEX_PACKAGE_DIR=dist            Directory for release archives.
#   ARTEX_COMPRESS=off                UPX mode: off, auto, or required.
#   ARTEX_UPX_ARGS="--best --lzma"    Arguments passed to UPX.
#   ARTEX_SKIP_FRONTEND=1             Reuse server/webui/dist (for CI artifact builds).
#   ARTEX_SKIP_NPM_CI=1               Skip npm ci while rebuilding the frontend.
#   ARTEX_GOSUMDB=sum.golang.org      Go checksum database.
set -euo pipefail
LANG_SEL="${ARTEX_LANGUAGE:-${ARTEX_LANGUAGE:-en}}"
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
ARTEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release로 대상 목록을 지정합니다.
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
Override targets with ARTEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release.
EOF
  fi
}

RELEASE_TARGETS_DEFAULT="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64"
ARTEX_RELEASE="${ARTEX_RELEASE:-0}"
ARTEX_COMPRESS="${ARTEX_COMPRESS:-off}"
ARTEX_PACKAGE="${ARTEX_PACKAGE:-0}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --release)
      ARTEX_RELEASE=1
      ARTEX_PACKAGE=1
      shift
      ;;
    --target)
      [ "$#" -ge 2 ] || die "$(msg "--target requires OS/ARCH" "--target에 OS/ARCH가 필요합니다")"
      target_arg="$2"
      case "$target_arg" in
        */*)
          ARTEX_TARGET_OS="${target_arg%%/*}"
          ARTEX_TARGET_ARCH="${target_arg##*/}"
          ARTEX_TARGETS="$target_arg"
          ;;
        *) die "$(msg "Target must be OS/ARCH, e.g. linux/amd64" "대상은 OS/ARCH 형식이어야 합니다 (예: linux/amd64)")" ;;
      esac
      shift 2
      ;;
    --no-compress)
      ARTEX_COMPRESS=0
      shift
      ;;
    --upx)
      ARTEX_COMPRESS=required
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

ARTEX_GOSUMDB="${ARTEX_GOSUMDB:-sum.golang.org}"
if [ -z "${ARTEX_BUILD_VERSION:-}" ]; then
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    ARTEX_BUILD_VERSION="$(git describe --tags --always --dirty)"
  else
    ARTEX_BUILD_VERSION="dev"
  fi
fi
# Release tags are commonly passed as v0.3.3; keep the binary version consistent.
ARTEX_BUILD_VERSION="${ARTEX_BUILD_VERSION#v}"
ARTEX_OUTPUT_DIR="${ARTEX_OUTPUT_DIR:-dist}"
ARTEX_PACKAGE_DIR="${ARTEX_PACKAGE_DIR:-$ARTEX_OUTPUT_DIR}"
ARTEX_UPX_ARGS="${ARTEX_UPX_ARGS:---best --lzma}"

if [ "${ARTEX_RELEASE}" = "1" ]; then
  ARTEX_TARGETS="${ARTEX_TARGETS:-$RELEASE_TARGETS_DEFAULT}"
else
  ARTEX_TARGET_OS="${ARTEX_TARGET_OS:-$(GOSUMDB="$ARTEX_GOSUMDB" go env GOOS)}"
  ARTEX_TARGET_ARCH="${ARTEX_TARGET_ARCH:-$(GOSUMDB="$ARTEX_GOSUMDB" go env GOARCH)}"
  ARTEX_TARGETS="${ARTEX_TARGETS:-${ARTEX_TARGET_OS}/${ARTEX_TARGET_ARCH}}"
fi

if [ "${ARTEX_SKIP_FRONTEND:-0}" = "1" ]; then
  [ -d server/webui/dist ] || die "$(msg "ARTEX_SKIP_FRONTEND=1 but server/webui/dist is missing" "ARTEX_SKIP_FRONTEND=1이지만 server/webui/dist가 없습니다")"
else
  command -v npm >/dev/null 2>&1 || die "$(msg "npm not found (Node.js/npm required for frontend export)" "npm을 찾을 수 없습니다 (프런트엔드 빌드에 Node.js/npm 필요)")"
  command -v rsync >/dev/null 2>&1 || die "$(msg "rsync not found" "rsync를 찾을 수 없습니다")"
  info "$(msg "Building frontend static assets" "프런트엔드 정적 파일 빌드 중")"
  if [ "${ARTEX_SKIP_NPM_CI:-0}" != "1" ]; then
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
  case "$ARTEX_COMPRESS" in
    0|off|false|none)
      info "$(msg "Skipping UPX: $binary" "UPX 건너뛰기: $binary")"
      return 0
      ;;
    auto|required|true|1) ;;
    *) die "$(msg "ARTEX_COMPRESS must be off, auto, or required" "ARTEX_COMPRESS는 off, auto 또는 required여야 합니다")" ;;
  esac

  if ! command -v upx >/dev/null 2>&1; then
    if [ "$ARTEX_COMPRESS" = "required" ]; then
      die "$(msg "ARTEX_COMPRESS=required but upx was not found" "ARTEX_COMPRESS=required이지만 upx를 찾을 수 없습니다")"
    fi
    warn "$(msg "upx not found; retaining linker output: $binary" "upx를 찾을 수 없어 linker 결과 유지: $binary")"
    return 0
  fi

  before=$(wc -c < "$binary" | tr -d ' ')
  upx_args="$ARTEX_UPX_ARGS"
  [ "$goos" = "darwin" ] && upx_args="$upx_args --force-macos"
  # shellcheck disable=SC2086
  if ! upx $upx_args -- "$binary"; then
    if [ "$ARTEX_COMPRESS" = "required" ]; then
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
  package_name="artex-${ARTEX_BUILD_VERSION}-${goos}-${goarch}"
  package_root="${ARTEX_PACKAGE_DIR}/${package_name}"
  archive="${ARTEX_PACKAGE_DIR}/${package_name}.zip"

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
  (cd "$ARTEX_PACKAGE_DIR" && zip -q -r -9 "$(basename "$archive")" "$(basename "$package_root")")
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

  binary_name="artex"
  [ "$goos" = "windows" ] && binary_name="artex.exe"
  if [ -n "${ARTEX_OUTPUT:-}" ] && [ "$ARTEX_RELEASE" != "1" ]; then
    output="$ARTEX_OUTPUT"
  else
    output="${ARTEX_OUTPUT_DIR}/artex-${goos}-${goarch}/${binary_name}"
  fi
  mkdir -p "$(dirname "$output")"

  info "$(msg "Building ${goos}/${goarch}, version ${ARTEX_BUILD_VERSION}" "${goos}/${goarch} 빌드 중, 버전 ${ARTEX_BUILD_VERSION}")"
  GOSUMDB="$ARTEX_GOSUMDB" \
  CGO_ENABLED=0 \
  GOOS="$goos" \
  GOARCH="$goarch" \
  go build \
    -tags embedui \
    -trimpath \
    -ldflags "-s -w -buildid= -X main.version=${ARTEX_BUILD_VERSION}" \
    -o "$output" \
    ./cmd/artex

  compress_binary "$output" "$goos"
  if command -v file >/dev/null 2>&1; then file "$output"; fi
  if [ "$ARTEX_PACKAGE" = "1" ]; then package_binary "$output" "$goos" "$goarch"; fi
  ok "$(msg "Build complete: $output" "빌드 완료: $output")"
}

write_checksums() {
  [ "$ARTEX_PACKAGE" = "1" ] || return 0
  checksum_file="$ARTEX_PACKAGE_DIR/SHA256SUMS"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$ARTEX_PACKAGE_DIR" && for archive in *.zip; do sha256sum "$archive"; done > "$(basename "$checksum_file")")
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$ARTEX_PACKAGE_DIR" && for archive in *.zip; do shasum -a 256 "$archive"; done > "$(basename "$checksum_file")")
  else
    warn "$(msg "sha256sum and shasum unavailable; skipping SHA256SUMS" "sha256sum과 shasum이 없어 SHA256SUMS 건너뛰기")"
    return 0
  fi
  ok "$(msg "Checksum file: $checksum_file" "체크섬 파일: $checksum_file")"
}

mkdir -p "$ARTEX_OUTPUT_DIR"
if [ "$ARTEX_PACKAGE" = "1" ]; then mkdir -p "$ARTEX_PACKAGE_DIR"; fi

old_ifs="$IFS"
IFS=','
read -r -a targets <<< "$ARTEX_TARGETS"
IFS="$old_ifs"
[ "${#targets[@]}" -gt 0 ] || die "$(msg "ARTEX_TARGETS cannot be empty" "ARTEX_TARGETS는 비어 있을 수 없습니다")"
for target in "${targets[@]}"; do
  target="${target//[[:space:]]/}"
  [ -n "$target" ] || continue
  build_target "$target"
done

if [ "$ARTEX_PACKAGE" = "1" ]; then
  write_checksums
  info "$(msg "Release archives generated in: $ARTEX_PACKAGE_DIR" "릴리스 압축 파일 생성 위치: $ARTEX_PACKAGE_DIR")"
fi

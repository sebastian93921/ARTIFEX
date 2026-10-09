#!/usr/bin/env bash
# ARTEX update: rebuild the local Docker image or native binary.
# Startup applies the idempotent database schema. Back up data before upgrading.
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"
LANG_SEL="${ARTEX_LANGUAGE:-${ARTEX_LANGUAGE:-en}}"
case "$LANG_SEL" in ko|ko_*|ko-*|KO) LANG_SEL=ko ;; *) LANG_SEL=en ;; esac
msg(){ if [ "$LANG_SEL" = ko ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
info(){ printf '%s\n' "$*"; }
die(){ printf '%s\n' "$*" >&2; exit 1; }
ask(){ local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }
sync_repo(){
  [ -d .git ] && command -v git >/dev/null 2>&1 || { info "$(msg 'Not a Git checkout; using current files' 'Git 작업 사본이 아니므로 현재 파일을 사용합니다')"; return; }
  [ "$(ask "$(msg 'Fetch current code with git pull --ff-only? (y/n)' 'git pull --ff-only로 최신 코드를 가져올까요? (y/n)')" y)" = y ] || return
  git pull --ff-only || die "$(msg 'Git cannot fast-forward. Resolve local changes or divergent history, then retry.' 'Git fast-forward 실패. 로컬 변경 또는 분기된 기록을 정리한 뒤 다시 시도하세요.')"
}
update_docker(){
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 || die "$(msg 'Install Docker and Docker Compose first' 'Docker와 Docker Compose를 먼저 설치하세요')"
  [ -f .env ] || die "$(msg 'Missing .env; run ./install.sh first' '.env가 없습니다. ./install.sh를 먼저 실행하세요')"
  info "$(msg 'Building ARTEX from this checkout…' '현재 소스로 ARTEX를 빌드합니다…')"
  docker compose build artex
  docker compose up -d artex
  info "$(msg 'Updated: http://localhost:8787. Logs: docker compose logs -f artex' '업데이트 완료: http://localhost:8787. 로그: docker compose logs -f artex')"
}
update_local(){
  command -v go >/dev/null 2>&1 || die "$(msg 'Go 1.26 or later required: https://go.dev/dl/' 'Go 1.26 이상 필요: https://go.dev/dl/')"
  if command -v npm >/dev/null 2>&1; then
    (cd web && npm ci && npm run build:static)
    mkdir -p server/webui/dist
    cp -R web/out/. server/webui/dist/
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    info "$(msg 'npm unavailable: building backend only; run the frontend separately' 'npm이 없어 백엔드만 빌드합니다. 프런트엔드는 별도로 실행하세요')"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  info "$(msg 'Built ./artex. Restart the running process to apply the update.' './artex 빌드 완료. 실행 중인 프로세스를 재시작하여 적용하세요.')"
}
echo "ARTEX"
echo "1) $(msg 'Rebuild Docker deployment' 'Docker 배포 다시 빌드')"
echo "2) $(msg 'Rebuild local binary' '로컬 바이너리 다시 빌드')"
case "$(ask "$(msg 'Choose' '선택')" 1)" in
  1) sync_repo; update_docker ;;
  2) sync_repo; update_local ;;
  *) die "$(msg 'Invalid choice' '잘못된 선택')" ;;
esac

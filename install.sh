#!/usr/bin/env bash
# ARTEX install script: ① all-in-Docker  ② build and run locally
#
# Messages are English by default. For Korean, set ARTEX_LANGUAGE=ko
# (ARTEX_LANGUAGE=ko also works) before running, e.g. ARTEX_LANGUAGE=ko ./install.sh
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

LANG_SEL="${ARTEX_LANGUAGE:-${ARTEX_LANGUAGE:-en}}"
case "$LANG_SEL" in ko|ko_*|ko-*|KO) LANG_SEL=ko ;; *) LANG_SEL=en ;; esac
# msg EN KO → print the string for the selected language.
msg(){ if [ "$LANG_SEL" = ko ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }
rand(){ head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; }

# ── docker environment detection / auto-install ─────────────
ensure_docker(){
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ok "$(msg 'Found docker and docker compose' 'docker와 docker compose를 감지했습니다')"; return
  fi
  warn "$(msg 'docker / docker compose not found' 'docker / docker compose를 찾지 못했습니다')"
  case "$(uname -s)" in
    Linux)
      if [ "$(ask "$(msg 'Install Docker automatically? (y/n)' 'Docker를 자동으로 설치할까요? (y/n)')" y)" = y ]; then
        curl -fsSL https://get.docker.com | sh
        sudo usermod -aG docker "$USER" || true
        ok "$(msg 'Docker installed (group change needs re-login to use without sudo)' 'Docker 설치 완료 (그룹 변경은 재로그인 후 sudo 없이 적용)')"
      else
        die "$(msg 'Please install docker yourself and retry' 'docker를 직접 설치한 뒤 다시 시도하세요')"
      fi ;;
    Darwin) die "$(msg 'On macOS install Docker Desktop: https://www.docker.com/products/docker-desktop/' 'macOS에서는 Docker Desktop을 설치하세요: https://www.docker.com/products/docker-desktop/')" ;;
    *)      die "$(msg 'Please install docker yourself and retry' 'docker를 직접 설치한 뒤 다시 시도하세요')" ;;
  esac
}

# ── ① all-in-Docker ─────────────────────────────────────────
install_docker(){
  ensure_docker
  if [ ! -f .env ]; then
    cp .env.example .env 2>/dev/null || true
    local pw key
    pw="$(ask "$(msg 'Postgres password (Enter for a random one)' 'Postgres 비밀번호 (Enter 시 무작위 생성)')" "$(rand)")"
    key="$(ask "$(msg 'ANTHROPIC_API_KEY (optional, can set later in the UI)' 'ANTHROPIC_API_KEY (선택, 나중에 UI에서 설정 가능)')" '')"
    sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${pw}|" .env
    sed -i.bak "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=${key}|" .env
    rm -f .env.bak
    ok "$(msg '.env generated (POSTGRES_PASSWORD set)' '.env 생성 완료 (POSTGRES_PASSWORD 설정됨)')"
  else
    info "$(msg 'Reusing existing .env' '기존 .env를 그대로 사용합니다')"
  fi
  info "$(msg 'Building ARTEX from this checkout…' '현재 소스로 ARTEX를 빌드합니다…')"
  docker compose build artex
  docker compose up -d
  ok "$(msg 'Started → http://localhost:8787' '시작 완료 → http://localhost:8787')"
  info "$(msg 'View logs: docker compose logs -f artex' '로그 보기: docker compose logs -f artex')"
}

# ── ② build and run locally ─────────────────────────────────
install_local(){
  echo "$(msg 'Database setup:' '데이터베이스 설치 방식:')"
  echo "  1) $(msg 'Connect to an existing PostgreSQL' '기존 PostgreSQL에 연결')"
  echo "  2) $(msg 'Start a PostgreSQL with Docker (needs docker)' 'Docker로 PostgreSQL 하나 띄우기 (docker 필요)')"
  case "$(ask "$(msg 'Choose' '선택')" 1)" in
    2)
      ensure_docker
      local pw; pw="$(ask "$(msg 'Postgres password (Enter for a random one)' 'Postgres 비밀번호 (Enter 시 무작위 생성)')" "$(rand)")"
      docker run -d --name artex-pg -p 5432:5432 \
        -e POSTGRES_USER=artex -e POSTGRES_PASSWORD="$pw" -e POSTGRES_DB=artex \
        -v artex-pg:/var/lib/postgresql/data postgres:16-alpine
      DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=artex DB_PASS="$pw" DB_NAME=artex DB_SSL=disable ;;
    *)
      DB_HOST="$(ask "$(msg 'Database host' '데이터베이스 주소')" 127.0.0.1)"
      DB_PORT="$(ask "$(msg 'Port' '포트')" 5432)"
      DB_USER="$(ask "$(msg 'User' '계정')" artex)"
      DB_PASS="$(ask "$(msg 'Password' '비밀번호')" '')"
      DB_NAME="$(ask "$(msg 'Database name' '데이터베이스 이름')" artex)"
      DB_SSL="$(ask "$(msg 'sslmode (disable/require)' 'sslmode (disable/require)')" disable)" ;;
  esac

  # Generate config.json
  cat > config.json <<JSON
{
  "database": {
    "host": "${DB_HOST}",
    "port": ${DB_PORT},
    "user": "${DB_USER}",
    "password": "${DB_PASS}",
    "dbname": "${DB_NAME}",
    "sslmode": "${DB_SSL}"
  }
}
JSON
  ok "$(msg 'config.json generated' 'config.json 생성 완료')"

  # go environment check
  command -v go >/dev/null 2>&1 || die "$(msg 'Go not found, install Go (>=1.26): https://go.dev/dl/' 'Go를 찾지 못했습니다. Go(>=1.26)를 먼저 설치하세요: https://go.dev/dl/')"
  ok "Go: $(go version)"

  # Embedding the frontend needs node to produce the static output
  if command -v npm >/dev/null 2>&1; then
    info "$(msg 'Building the frontend static output…' '프런트엔드 정적 산출물을 빌드합니다…')"
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "$(msg 'Compiling the single binary with the frontend embedded…' '프런트엔드가 내장된 단일 바이너리를 컴파일합니다…')"
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "$(msg 'npm not found: building the backend WITHOUT the embedded frontend (run npm run dev for the frontend)' 'npm을 찾지 못함: 프런트엔드 미내장 백엔드를 빌드합니다 (프런트엔드는 따로 npm run dev)')"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "$(msg 'Build complete → ./artex' '컴파일 완료 → ./artex')"

  info "$(msg 'Starting… (Ctrl-C to quit)' '시작합니다… (Ctrl-C로 종료)')"
  ./start.sh
}

echo "=============================="
echo "  $(msg 'ARTEX install' 'ARTEX 설치')"
echo "  1) $(msg 'All-in-Docker install' '전부 Docker로 설치')"
echo "  2) $(msg 'Local run (go build)' '로컬 실행 (go 컴파일)')"
echo "=============================="
case "$(ask "$(msg 'Choose' '선택')" 1)" in
  1) install_docker ;;
  2) install_local ;;
  *) die "$(msg 'Invalid choice' '잘못된 선택')" ;;
esac

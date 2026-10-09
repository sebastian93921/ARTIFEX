#!/usr/bin/env bash
# Reset the administrator password stored as bcrypt in settings/auth.password_hash.
# Local connection precedence: CLI fields, --dsn/ARTIFEX_PG_DSN, config.json.
# Docker mode runs psql inside the PostgreSQL container (no published port needed).
# The new password is passed through the environment and psql \getenv, not argv.
# psql :'newpw' quotes the value safely; pgcrypto generates a compatible bcrypt hash.
set -euo pipefail
LANG_SEL="${ARTIFEX_LANGUAGE:-${ARTIFEX_LANGUAGE:-en}}"
case "$LANG_SEL" in ko|ko_*|ko-*|KO) LANG_SEL=ko ;; *) LANG_SEL=en ;; esac
msg(){ if [ "$LANG_SEL" = ko ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }


PASS_KEY="auth.password_hash"
BCRYPT_COST=10

MODE=""            # local | docker (empty = automatic)
DSN=""
HOST="" PORT="" USER="" DBPASS="" DBNAME="" SSLMODE=""
CONFIG=""
CONTAINER=""       # PostgreSQL service/container name (default postgres)
EXEC_KIND=""       # compose | docker (empty = automatic)
NEWPASS=""
ASSUME_YES=0

die() { echo "$(msg "Error: $*" "오류: $*")" >&2; exit 1; }
info() { echo "· $*" >&2; }

usage() {
  echo "$(msg 'Reset ARTIFEX administrator password (legacy username ARTIFEX).' 'ARTIFEX 관리자 비밀번호 재설정 (기존 사용자 이름 ARTIFEX).')"
  echo "$(msg 'Usage: ./reset-password.sh [options]; prompts for a password when omitted.' '사용법: ./reset-password.sh [옵션]; 비밀번호 생략 시 입력을 요청합니다.')"
  echo '-m/--mode local|docker; --dsn DSN; --config PATH; --sslmode MODE'
  echo '-H/--host HOST; -P/--port PORT; -U/--user USER; -W/--db-password PASSWORD'
  echo '-d/--dbname DATABASE; -c/--container NAME; --exec compose|docker'
  echo '-p/--new-password PASSWORD; -y/--yes; -h/--help'
  exit 0
}

# Parse arguments.
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--mode)        MODE="${2:-}"; shift 2 ;;
    --dsn)            DSN="${2:-}"; shift 2 ;;
    -H|--host)        HOST="${2:-}"; shift 2 ;;
    -P|--port)        PORT="${2:-}"; shift 2 ;;
    -U|--user)        USER="${2:-}"; shift 2 ;;
    -W|--db-password) DBPASS="${2:-}"; shift 2 ;;
    -d|--dbname)      DBNAME="${2:-}"; shift 2 ;;
    --sslmode)        SSLMODE="${2:-}"; shift 2 ;;
    --config)         CONFIG="${2:-}"; shift 2 ;;
    -c|--container)   CONTAINER="${2:-}"; shift 2 ;;
    --exec)           EXEC_KIND="${2:-}"; shift 2 ;;
    -p|--new-password) NEWPASS="${2:-}"; shift 2 ;;
    -y|--yes)         ASSUME_YES=1; shift ;;
    -h|--help)        usage ;;
    *) die "$(msg "Unknown argument: $1 (see -h)" "알 수 없는 인수: $1 (-h 참고)")" ;;
  esac
done

# Read database fields from config.json in local mode when not explicitly supplied.
# Prefer Python JSON parsing; the fallback supports simple string/number fields.
read_config_json() {
  local path="$1"
  [[ -f "$path" ]] || return 1
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$path" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1])).get("database", {})
except Exception:
    sys.exit(1)
# Accept either a DSN or individual connection fields.
if d.get("dsn"):
    print("DSN\t" + d["dsn"]); sys.exit(0)
for k in ("host","port","user","password","dbname","sslmode"):
    if d.get(k) is not None:
        print(k.upper() + "\t" + str(d[k]))
PY
  else
    # Minimal fallback: extract each string or numeric field.
    local k
    for k in host port user password dbname sslmode; do
      local v
      v=$(grep -oE "\"$k\"[[:space:]]*:[[:space:]]*(\"[^\"]*\"|[0-9]+)" "$path" 2>/dev/null \
            | head -1 | sed -E "s/.*:[[:space:]]*//; s/^\"//; s/\"$//") || true
      [[ -n "$v" ]] && echo -e "${k^^}\t$v"
    done
  fi
}

apply_config_fields() {
  local line key val
  while IFS=$'\t' read -r key val; do
    [[ -z "$key" ]] && continue
    case "$key" in
      DSN)      [[ -z "$DSN" ]] && DSN="$val" ;;
      HOST)     [[ -z "$HOST" ]] && HOST="$val" ;;
      PORT)     [[ -z "$PORT" ]] && PORT="$val" ;;
      USER)     [[ -z "$USER" ]] && USER="$val" ;;
      PASSWORD) [[ -z "$DBPASS" ]] && DBPASS="$val" ;;
      DBNAME)   [[ -z "$DBNAME" ]] && DBNAME="$val" ;;
      SSLMODE)  [[ -z "$SSLMODE" ]] && SSLMODE="$val" ;;
    esac
  done
}

# Detect deployment mode.
if [[ -z "$MODE" ]]; then
  if [[ -n "$DSN$HOST$USER$DBNAME" || -n "${ARTIFEX_PG_DSN:-}" || -f "${CONFIG:-config.json}" ]]; then
    MODE="local"
  elif command -v docker >/dev/null 2>&1 && [[ -f docker-compose.yml ]]; then
    MODE="docker"
  else
    MODE="local"
  fi
fi
info "$(msg "Deployment mode: $MODE" "배포 방식: $MODE")"

# Collect and confirm the new password.
if [[ -z "$NEWPASS" ]]; then
  read -r -s -p "$(msg "New password (legacy username ARTIFEX): " "새 비밀번호 (기존 사용자 이름 ARTIFEX): ")" NEWPASS; echo >&2
  [[ -n "$NEWPASS" ]] || die "$(msg "Password cannot be empty" "비밀번호는 비어 있을 수 없습니다")"
  read -r -s -p "$(msg "Confirm password: " "비밀번호 확인: ")" NEWPASS2; echo >&2
  [[ "$NEWPASS" == "$NEWPASS2" ]] || die "$(msg "Passwords do not match" "비밀번호가 일치하지 않습니다")"
fi
[[ -n "$NEWPASS" ]] || die "$(msg "Password cannot be empty" "비밀번호는 비어 있을 수 없습니다")"

# Pass the password through the environment for psql \getenv.
export ARTIFEX_RESET_NEWPASS="$NEWPASS"

# Generate bcrypt and upsert in the database; psql quotes the password safely.
# CREATE EXTENSION is idempotent but requires an authorized database role.
SQL=$(cat <<SQL
\\set ON_ERROR_STOP on
\\getenv newpw ARTIFEX_RESET_NEWPASS
CREATE EXTENSION IF NOT EXISTS pgcrypto;
INSERT INTO settings(key, value)
VALUES ('$PASS_KEY', crypt(:'newpw', gen_salt('bf', $BCRYPT_COST)))
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
SQL
)

# Execute the reset.
if [[ "$MODE" == "local" ]]; then
  # Connection precedence: CLI, --dsn/ARTIFEX_PG_DSN, config.json.
  if [[ -z "$DSN" && -z "$HOST$USER$DBNAME" ]]; then
    [[ -n "${ARTIFEX_PG_DSN:-}" ]] && DSN="$ARTIFEX_PG_DSN"
  fi
  if [[ -z "$DSN" && -z "$HOST$USER$DBNAME" ]]; then
    cfg="${CONFIG:-config.json}"
    if [[ -f "$cfg" ]]; then
      info "$(msg "Reading database configuration from $cfg" "$cfg에서 데이터베이스 설정을 읽습니다")"
      apply_config_fields < <(read_config_json "$cfg")
    fi
  fi

  command -v psql >/dev/null 2>&1 || die "$(msg "psql not found; install postgresql-client or use -m docker" "psql이 없습니다. postgresql-client를 설치하거나 -m docker를 사용하세요")"

  declare -a PSQL_ARGS=()
  if [[ -n "$DSN" ]]; then
    PSQL_ARGS=("$DSN")
    target="$DSN"
  else
    [[ -n "$USER"   ]] || die "$(msg "Missing database user (-U) or valid config.json/DSN" "데이터베이스 사용자(-U) 또는 유효한 config.json/DSN이 필요합니다")"
    [[ -n "$DBNAME" ]] || die "$(msg "Missing database name (-d) or valid config.json/DSN" "데이터베이스 이름(-d) 또는 유효한 config.json/DSN이 필요합니다")"
    HOST="${HOST:-127.0.0.1}"; PORT="${PORT:-5432}"; SSLMODE="${SSLMODE:-disable}"
    PSQL_ARGS=(-h "$HOST" -p "$PORT" -U "$USER" -d "$DBNAME")
    [[ -n "$SSLMODE" ]] && export PGSSLMODE="$SSLMODE"
    [[ -n "$DBPASS" ]] && export PGPASSWORD="$DBPASS"
    target="$USER@$HOST:$PORT/$DBNAME"
  fi

  info "$(msg "Target database: $target" "대상 데이터베이스: $target")"
  if [[ "$ASSUME_YES" -ne 1 ]]; then
    read -r -p "$(msg "Reset the administrator password in this database? [y/N] " "이 데이터베이스에서 관리자 비밀번호를 재설정할까요? [y/N] ")" ans
    [[ "$ans" == "y" || "$ans" == "Y" ]] || die "$(msg "Cancelled" "취소됨")"
  fi

  if ! printf '%s\n' "$SQL" | psql "${PSQL_ARGS[@]}" -v ON_ERROR_STOP=1 -q >/dev/null; then
    die "$(msg "Write failed. For pgcrypto permission/missing errors, use a role allowed to create extensions or run CREATE EXTENSION pgcrypto first." "쓰기 실패. pgcrypto 권한/누락 오류라면 확장 생성 권한이 있는 계정을 사용하거나 CREATE EXTENSION pgcrypto를 먼저 실행하세요.")"
  fi

else
  # ---- docker ----
  command -v docker >/dev/null 2>&1 || die "$(msg "docker not found" "docker를 찾을 수 없습니다")"
  CONTAINER="${CONTAINER:-postgres}"

  # Prefer docker compose exec by service name; otherwise docker exec by container.
  if [[ -z "$EXEC_KIND" ]]; then
    if docker compose version >/dev/null 2>&1 && [[ -f docker-compose.yml ]]; then
      EXEC_KIND="compose"
    else
      EXEC_KIND="docker"
    fi
  fi

  # Credentials: CLI, POSTGRES_* from .env, then compose defaults (artifex).
  if [[ -f .env ]]; then
    # shellcheck disable=SC1091
    set -a; . ./.env; set +a
  fi
  DUSER="${USER:-${POSTGRES_USER:-artifex}}"
  DNAME="${DBNAME:-${POSTGRES_DB:-artifex}}"
  [[ -n "$DBPASS" ]] && export PGPASSWORD="$DBPASS"
  [[ -z "${PGPASSWORD:-}" && -n "${POSTGRES_PASSWORD:-}" ]] && export PGPASSWORD="$POSTGRES_PASSWORD"

  info "$(msg "Target: container $CONTAINER, psql -U $DUSER -d $DNAME (exec=$EXEC_KIND)" "대상: 컨테이너 $CONTAINER, psql -U $DUSER -d $DNAME (exec=$EXEC_KIND)")"
  if [[ "$ASSUME_YES" -ne 1 ]]; then
    read -r -p "$(msg "Reset the administrator password in this container database? [y/N] " "이 컨테이너 데이터베이스에서 관리자 비밀번호를 재설정할까요? [y/N] ")" ans
    [[ "$ans" == "y" || "$ans" == "Y" ]] || die "$(msg "Cancelled" "취소됨")"
  fi

  # Pass only variable names to -e so secrets stay out of docker argv.
  declare -a EXEC_CMD
  if [[ "$EXEC_KIND" == "compose" ]]; then
    EXEC_CMD=(docker compose exec -T -e ARTIFEX_RESET_NEWPASS -e PGPASSWORD "$CONTAINER"
              psql -U "$DUSER" -d "$DNAME" -v ON_ERROR_STOP=1 -q)
  else
    EXEC_CMD=(docker exec -i -e ARTIFEX_RESET_NEWPASS -e PGPASSWORD "$CONTAINER"
              psql -U "$DUSER" -d "$DNAME" -v ON_ERROR_STOP=1 -q)
  fi

  if ! printf '%s\n' "$SQL" | "${EXEC_CMD[@]}" >/dev/null; then
    die "$(msg "Write failed. Check container (-c), database credentials (POSTGRES_* in .env), and pgcrypto permissions." "쓰기 실패. 컨테이너(-c), 데이터베이스 계정(.env의 POSTGRES_*), pgcrypto 권한을 확인하세요.")"
  fi
fi

unset ARTIFEX_RESET_NEWPASS
echo "$(msg "Administrator password reset. Sign in as ARTIFEX with the new password; no restart needed." "관리자 비밀번호를 재설정했습니다. ARTIFEX와 새 비밀번호로 로그인하세요. 재시작은 필요하지 않습니다.")"

#!/bin/sh
# ARTIFEX supervisor: exit 0 stops, 75 restarts immediately, other codes back off.
# Forward termination signals and wait for the child to finish graceful shutdown.
# Download verification and atomic replacement are handled by selfupdate.Bootstrap.
set -u
LANG_SEL="${ARTIFEX_LANGUAGE:-${ARTIFEX_LANGUAGE:-en}}"
case "$LANG_SEL" in ko|ko_*|ko-*|KO) LANG_SEL=ko ;; *) LANG_SEL=en ;; esac
msg(){ if [ "$LANG_SEL" = ko ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }


cd "$(dirname "$0")" || exit 1

BIN=./artifex
[ -x "$BIN" ] || { echo "[ARTIFEX] $(msg "Executable not found: $BIN" "실행 파일을 찾을 수 없습니다: $BIN")" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

forward() {
	stopping=1
	if [ "$child" -ne 0 ]; then
		kill -TERM "$child" 2>/dev/null || true
	fi
}
trap forward INT TERM

delay=1
while :; do
	"$BIN" "$@" &
	child=$!

	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[ARTIFEX] $(msg "Stopped" "중지됨")"
		exit 0
	fi

	case "$code" in
		0)
			echo "[ARTIFEX] $(msg "Exited normally" "정상 종료")"
			exit 0
			;;
		"$RESTART_CODE")
			echo "[ARTIFEX] $(msg "Restart requested (applying update)…" "다시 시작 요청 (업데이트 적용 중)…")"
			delay=1
			;;
		*)
			echo "[ARTIFEX] $(msg "Exited with code $code; restarting in ${delay}s" "종료 코드 $code; ${delay}초 후 다시 시작")" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done

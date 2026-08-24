#!/usr/bin/env bash
# Resolves the M365Bridge master passphrase from the host OS keyring (or a
# generated fallback file if no keyring backend is available), then execs the
# given command with M365_MASTER_PASSPHRASE_VALUE set for that command alone.
#
# Usage:
#   scripts/with-passphrase.sh docker compose up -d
#   scripts/with-passphrase.sh ./bin/m365-bridge serve
#   scripts/with-passphrase.sh go run ./cmd/cli serve
#
# The passphrase never touches a persistent shell variable, a file this
# script owns beyond the fallback path below, or docker-compose.yml. Each
# backend is tried in order; the first one available is used exclusively, so
# the passphrase always ends up in exactly one place.
set -euo pipefail

readonly SERVICE="m365bridge"
readonly ACCOUNT="${USER:-m365bridge}"
readonly FALLBACK_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/m365bridge"
readonly FALLBACK_FILE="$FALLBACK_DIR/passphrase.txt"

generate_passphrase() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -base64 32
	else
		head -c 32 /dev/urandom | base64
	fi
}

# Each try_* function prints the passphrase on stdout and returns 0 when its
# backend is usable, generating and storing a new passphrase on first use.
# It returns 1 without printing anything when the backend itself is missing,
# so the caller's `||` chain falls through to the next candidate.

# intercept_for_fallback_test lets the plaintext-fallback path be exercised on
# demand, without uninstalling a keyring backend or editing this script. When
# TEST_FALLBACK_MECHANISM is set to any non-empty value, it goes straight to
# fallback_plaintext_file and succeeds, so it is first in the `||` chain below
# and short-circuits before any real backend is tried. It never touches PATH,
# the environment the launched command sees, or a real keyring/pass store.
intercept_for_fallback_test() {
	[[ -n "${TEST_FALLBACK_MECHANISM:-}" ]] || return 1
	echo "TEST_FALLBACK_MECHANISM is set: forcing the plaintext fallback path." >&2
	local secret
	secret="$(fallback_plaintext_file)"
	printf '%s' "$secret"
	return 0
}

try_macos_keychain() {
	command -v security >/dev/null 2>&1 || return 1
	local existing
	if existing="$(security find-generic-password -a "$ACCOUNT" -s "$SERVICE" -w 2>/dev/null)"; then
		printf '%s' "$existing"
		return 0
	fi
	local pass
	pass="$(generate_passphrase)"
	if ! security add-generic-password -a "$ACCOUNT" -s "$SERVICE" -w "$pass" -U >/dev/null 2>&1; then
		echo "WARNING: could not store the passphrase in the macOS Keychain; trying the next backend." >&2
		return 1
	fi
	printf '%s' "$pass"
}

try_secret_tool() {
	command -v secret-tool >/dev/null 2>&1 || return 1
	local existing
	if existing="$(secret-tool lookup service "$SERVICE" account "$ACCOUNT" 2>/dev/null)"; then
		printf '%s' "$existing"
		return 0
	fi
	local pass
	pass="$(generate_passphrase)"
	if ! printf '%s' "$pass" | secret-tool store --label="M365Bridge master passphrase" service "$SERVICE" account "$ACCOUNT" >/dev/null 2>&1; then
		echo "WARNING: could not store the passphrase via secret-tool; trying the next backend." >&2
		return 1
	fi
	printf '%s' "$pass"
}

try_pass() {
	command -v pass >/dev/null 2>&1 || return 1
	local existing
	if existing="$(pass show "$SERVICE/passphrase" 2>/dev/null)"; then
		printf '%s' "$existing"
		return 0
	fi
	local p
	p="$(generate_passphrase)"
	if ! printf '%s\n' "$p" | pass insert -m -f "$SERVICE/passphrase" >/dev/null 2>&1; then
		echo "WARNING: could not store the passphrase via pass; trying the next backend." >&2
		return 1
	fi
	printf '%s' "$p"
}

fallback_plaintext_file() {
	if [[ ! -f "$FALLBACK_FILE" ]]; then
		mkdir -p "$FALLBACK_DIR"
		generate_passphrase >"$FALLBACK_FILE"
		chmod 600 "$FALLBACK_FILE"
		{
			echo "WARNING: no OS keyring backend found (tried security, secret-tool, pass)."
			echo "WARNING: the master passphrase was generated and saved in the clear at:"
			echo "WARNING:   $FALLBACK_FILE"
			echo "WARNING: install a keyring backend (macOS Keychain is built in; on Linux"
			echo "WARNING: install gnome-keyring/libsecret for secret-tool, or use pass)"
			echo "WARNING: for real protection."
		} >&2
	fi
	cat "$FALLBACK_FILE"
}

if [[ $# -eq 0 ]]; then
	echo "usage: $0 <command> [args...]" >&2
	exit 64
fi

passphrase="$(intercept_for_fallback_test || try_macos_keychain || try_secret_tool || try_pass || fallback_plaintext_file)"

M365_MASTER_PASSPHRASE_VALUE="$passphrase" exec "$@"

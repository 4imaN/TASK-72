#!/usr/bin/env sh
# bootstrap-runtime.sh — generates runtime secrets on first boot
# Writes to /runtime/secrets (Docker named volume or bind-mount)
# NEVER echoes plaintext secrets to stdout.
set -eu

SECRETS_DIR="${SECRETS_DIR:-/runtime/secrets}"
MANIFEST="$SECRETS_DIR/bootstrap-manifest.json"

log() { printf '[bootstrap] %s\n' "$1" >&2; }

gen_secret() {
  # 32 bytes = 256-bit hex
  head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
}

gen_password() {
  # 24 chars URL-safe base64
  head -c 18 /dev/urandom | base64 | tr '+/' '-_' | tr -d '\n'
}

mkdir -p "$SECRETS_DIR"
chmod 700 "$SECRETS_DIR"

# ── database superpassword ───────────────────────────────────────────────────
if [ ! -f "$SECRETS_DIR/db_superpassword.txt" ]; then
  gen_password > "$SECRETS_DIR/db_superpassword.txt"
  chmod 600 "$SECRETS_DIR/db_superpassword.txt"
  log "Generated db_superpassword"
else
  log "db_superpassword already exists — skipping"
fi

# ── database app-user password ───────────────────────────────────────────────
if [ ! -f "$SECRETS_DIR/db_apppassword.txt" ]; then
  gen_password > "$SECRETS_DIR/db_apppassword.txt"
  chmod 600 "$SECRETS_DIR/db_apppassword.txt"
  log "Generated db_apppassword"
fi

# ── session signing key ───────────────────────────────────────────────────────
if [ ! -f "$SECRETS_DIR/session_key.txt" ]; then
  gen_secret > "$SECRETS_DIR/session_key.txt"
  chmod 600 "$SECRETS_DIR/session_key.txt"
  log "Generated session_key"
fi

# ── encryption master key ────────────────────────────────────────────────────
if [ ! -f "$SECRETS_DIR/encryption_key.txt" ]; then
  gen_secret > "$SECRETS_DIR/encryption_key.txt"
  chmod 600 "$SECRETS_DIR/encryption_key.txt"
  log "Generated encryption_key"
fi

# ── TOTP recovery encryption key ─────────────────────────────────────────────
if [ ! -f "$SECRETS_DIR/totp_recovery_key.txt" ]; then
  gen_secret > "$SECRETS_DIR/totp_recovery_key.txt"
  chmod 600 "$SECRETS_DIR/totp_recovery_key.txt"
  log "Generated totp_recovery_key"
fi

# ── LAN webhook signing key ───────────────────────────────────────────────────
if [ ! -f "$SECRETS_DIR/webhook_signing_key.txt" ]; then
  gen_secret > "$SECRETS_DIR/webhook_signing_key.txt"
  chmod 600 "$SECRETS_DIR/webhook_signing_key.txt"
  log "Generated webhook_signing_key"
fi

# ── bootstrap account passwords (generated once, printed to manifest) ─────────
for role in learner procurement approver finance moderator admin; do
  f="$SECRETS_DIR/bootstrap_pw_${role}.txt"
  if [ ! -f "$f" ]; then
    gen_password > "$f"
    chmod 600 "$f"
    log "Generated bootstrap password for $role"
  fi
done

# ── write bootstrap manifest ─────────────────────────────────────────────────
# Machine-readable JSON with file paths only (no plaintext values)
cat > "$MANIFEST" <<EOF
{
  "version": 1,
  "secrets_dir": "$SECRETS_DIR",
  "files": {
    "db_superpassword":      "$SECRETS_DIR/db_superpassword.txt",
    "db_apppassword":        "$SECRETS_DIR/db_apppassword.txt",
    "session_key":           "$SECRETS_DIR/session_key.txt",
    "encryption_key":        "$SECRETS_DIR/encryption_key.txt",
    "totp_recovery_key":     "$SECRETS_DIR/totp_recovery_key.txt",
    "webhook_signing_key":   "$SECRETS_DIR/webhook_signing_key.txt",
    "bootstrap_pw_learner":  "$SECRETS_DIR/bootstrap_pw_learner.txt",
    "bootstrap_pw_procurement": "$SECRETS_DIR/bootstrap_pw_procurement.txt",
    "bootstrap_pw_approver": "$SECRETS_DIR/bootstrap_pw_approver.txt",
    "bootstrap_pw_finance":  "$SECRETS_DIR/bootstrap_pw_finance.txt",
    "bootstrap_pw_moderator":"$SECRETS_DIR/bootstrap_pw_moderator.txt",
    "bootstrap_pw_admin":    "$SECRETS_DIR/bootstrap_pw_admin.txt"
  }
}
EOF
chmod 640 "$MANIFEST"
log "Bootstrap manifest written to $MANIFEST"
log "Bootstrap complete."

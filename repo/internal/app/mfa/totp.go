// Package mfa provides TOTP-based multi-factor authentication with recovery codes.
package mfa

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"portal/internal/platform/crypto"
)

const (
	// Issuer is the TOTP issuer name shown in authenticator apps.
	Issuer = "Portal"
	// RecoveryCodeCount is the number of one-time recovery codes generated on enrollment.
	RecoveryCodeCount = 8
)

// Store manages TOTP enrollment and recovery code persistence.
type Store struct {
	pool      *pgxpool.Pool
	encryptor *crypto.Encryptor
}

// NewStore constructs a Store backed by the given pool and encryptor.
func NewStore(pool *pgxpool.Pool, enc *crypto.Encryptor) *Store {
	return &Store{pool: pool, encryptor: enc}
}

// TOTPEnrollment holds the QR provisioning URI and secret for display once.
type TOTPEnrollment struct {
	ProvisioningURI string
	SecretPlaintext string // shown once during enrollment, never stored plaintext
}

// StartEnrollment generates a new TOTP key and stores the encrypted secret (unconfirmed).
func (s *Store) StartEnrollment(ctx context.Context, userID, username string) (*TOTPEnrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      Issuer,
		AccountName: username,
		Algorithm:   otp.AlgorithmSHA1,
		Digits:      otp.DigitsSix,
		Period:      30,
	})
	if err != nil {
		return nil, fmt.Errorf("generate totp key: %w", err)
	}

	encryptedSecret, err := s.encryptor.Encrypt(key.Secret())
	if err != nil {
		return nil, fmt.Errorf("encrypt totp secret: %w", err)
	}

	// Upsert: replace any existing unconfirmed enrollment.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO mfa_totp_enrollments (id, user_id, encrypted_secret, confirmed)
		VALUES ($1, $2, $3, FALSE)
		ON CONFLICT (user_id) DO UPDATE
			SET encrypted_secret = EXCLUDED.encrypted_secret, confirmed = FALSE`,
		uuid.New().String(), userID, encryptedSecret,
	)
	if err != nil {
		return nil, fmt.Errorf("store totp enrollment: %w", err)
	}

	return &TOTPEnrollment{
		ProvisioningURI: key.URL(),
		SecretPlaintext: key.Secret(),
	}, nil
}

// ConfirmEnrollment verifies the user-provided code and marks the enrollment confirmed.
func (s *Store) ConfirmEnrollment(ctx context.Context, userID, code string) error {
	secret, err := s.getDecryptedSecret(ctx, userID)
	if err != nil {
		return err
	}

	if !totp.Validate(code, secret) {
		return fmt.Errorf("invalid TOTP code")
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE mfa_totp_enrollments SET confirmed = TRUE, last_used_at = NOW()
		WHERE user_id = $1`, userID)
	return err
}

// Verify checks a TOTP code against the confirmed enrollment.
func (s *Store) Verify(ctx context.Context, userID, code string) error {
	var confirmed bool
	err := s.pool.QueryRow(ctx, `SELECT confirmed FROM mfa_totp_enrollments WHERE user_id = $1`, userID).Scan(&confirmed)
	if err != nil {
		return fmt.Errorf("enrollment not found")
	}
	if !confirmed {
		return fmt.Errorf("MFA not confirmed")
	}

	secret, err := s.getDecryptedSecret(ctx, userID)
	if err != nil {
		return err
	}

	valid, _ := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      1, // ±1 window for clock drift
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if !valid {
		return fmt.Errorf("invalid TOTP code")
	}

	_, _ = s.pool.Exec(ctx, `UPDATE mfa_totp_enrollments SET last_used_at = NOW() WHERE user_id = $1`, userID)
	return nil
}

// IsEnrolled returns true if the user has a confirmed TOTP enrollment.
func (s *Store) IsEnrolled(ctx context.Context, userID string) (bool, error) {
	var confirmed bool
	err := s.pool.QueryRow(ctx, `SELECT confirmed FROM mfa_totp_enrollments WHERE user_id = $1`, userID).Scan(&confirmed)
	if err != nil {
		return false, nil
	}
	return confirmed, nil
}

// GenerateRecoveryCodes creates 8 recovery codes, stores bcrypt hashes, returns plaintexts.
func (s *Store) GenerateRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes := make([]string, RecoveryCodeCount)

	// Delete old codes.
	_, _ = s.pool.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID)

	for i := range codes {
		raw, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes[i] = raw
		hash, err := bcrypt.GenerateFromPassword([]byte(raw), 10)
		if err != nil {
			return nil, err
		}
		_, err = s.pool.Exec(ctx, `
			INSERT INTO mfa_recovery_codes (id, user_id, code_hash)
			VALUES ($1, $2, $3)`,
			uuid.New().String(), userID, string(hash),
		)
		if err != nil {
			return nil, fmt.Errorf("store recovery code: %w", err)
		}
	}
	return codes, nil
}

// UseRecoveryCode checks a recovery code and marks it used.
func (s *Store) UseRecoveryCode(ctx context.Context, userID, code string) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code_hash FROM mfa_recovery_codes
		WHERE user_id = $1 AND used_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("query recovery codes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
			_, _ = s.pool.Exec(ctx, `UPDATE mfa_recovery_codes SET used_at = NOW() WHERE id = $1`, id)
			return nil
		}
	}
	return fmt.Errorf("invalid or already used recovery code")
}

func (s *Store) getDecryptedSecret(ctx context.Context, userID string) (string, error) {
	var encSecret string
	err := s.pool.QueryRow(ctx, `SELECT encrypted_secret FROM mfa_totp_enrollments WHERE user_id = $1`, userID).Scan(&encSecret)
	if err != nil {
		return "", fmt.Errorf("enrollment not found")
	}
	return s.encryptor.Decrypt(encSecret)
}

func generateRecoveryCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil // 12-char hex recovery code
}

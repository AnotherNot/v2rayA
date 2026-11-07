package jwt

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/v2rayA/v2rayA/db"
)

const (
	// AccessTTL is short because the token sits in browser storage. A
	// refresh token, rotated on every use, carries the long-lived session.
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 30 * 24 * time.Hour
)

var ErrSessionInvalid = errors.New("the session is invalid, expired or revoked")

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession stores a refresh token and returns the session id that the
// access token carries as jti, plus the refresh token itself.
func CreateSession(username string) (id, refresh string, err error) {
	CleanupSessions()
	if id, err = randomHex(16); err != nil {
		return "", "", err
	}
	if refresh, err = randomRefreshToken(); err != nil {
		return "", "", err
	}
	now := time.Now()
	_, err = db.GetDB().Exec(
		`INSERT INTO sessions (id, username, refresh_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		id, username, hashRefreshToken(refresh), now.Unix(), now.Add(RefreshTTL).Unix(),
	)
	if err != nil {
		return "", "", err
	}
	return id, refresh, nil
}

// RefreshSession validates a refresh token, rotates it, and returns the
// session's identity. The old token stops working immediately.
func RefreshSession(refresh string) (id, username, newRefresh string, err error) {
	if refresh == "" {
		return "", "", "", ErrSessionInvalid
	}
	var expiresAt int64
	var revokedAt sql.NullInt64
	err = db.GetDB().QueryRow(
		`SELECT id, username, expires_at, revoked_at FROM sessions WHERE refresh_hash = ?`,
		hashRefreshToken(refresh),
	).Scan(&id, &username, &expiresAt, &revokedAt)
	if err == sql.ErrNoRows {
		return "", "", "", ErrSessionInvalid
	}
	if err != nil {
		return "", "", "", err
	}
	if revokedAt.Valid || time.Now().Unix() >= expiresAt {
		return "", "", "", ErrSessionInvalid
	}
	if newRefresh, err = randomRefreshToken(); err != nil {
		return "", "", "", err
	}
	_, err = db.GetDB().Exec(
		`UPDATE sessions SET refresh_hash = ?, expires_at = ? WHERE id = ?`,
		hashRefreshToken(newRefresh), time.Now().Add(RefreshTTL).Unix(), id,
	)
	if err != nil {
		return "", "", "", err
	}
	return id, username, newRefresh, nil
}

// SessionValid reports whether an access token's jti may still be used.
func SessionValid(id string) bool {
	if id == "" {
		return false
	}
	var one int
	err := db.GetDB().QueryRow(
		`SELECT 1 FROM sessions WHERE id = ? AND revoked_at IS NULL AND expires_at > ?`,
		id, time.Now().Unix(),
	).Scan(&one)
	return err == nil
}

func RevokeSession(id string) error {
	if id == "" {
		return nil
	}
	_, err := db.GetDB().Exec(
		`UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().Unix(), id,
	)
	return err
}

func RevokeAllSessions() error {
	_, err := db.GetDB().Exec(
		`UPDATE sessions SET revoked_at = ? WHERE revoked_at IS NULL`,
		time.Now().Unix(),
	)
	return err
}

// CleanupSessions drops rows that cannot become valid again. It runs on
// login; the table stays small without a background goroutine.
func CleanupSessions() {
	cutoff := time.Now().Add(-RefreshTTL).Unix()
	_, _ = db.GetDB().Exec(
		`DELETE FROM sessions WHERE expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)`,
		cutoff, cutoff,
	)
}

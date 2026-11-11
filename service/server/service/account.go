package service

import (
	"fmt"
	"time"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
)

func Login(username, password string) (token, refreshToken string, err error) {
	hash, hashErr := configure.GetPasswordOfAccount(username)
	if hashErr != nil {
		return "", "", common.Coded("WRONG_CREDENTIALS", fmt.Errorf("wrong username or password"), nil)
	}
	// The MD5-derived scheme older releases stored is not verified at all. The
	// message names the way out, and the account still counts as existing, so
	// an upgrade does not fall back to the first-run registration.
	if configure.IsLegacyAccountHash(hash) {
		return "", "", common.Coded("LEGACY_PASSWORD_HASH", fmt.Errorf("this account's password was stored with a scheme that is no longer accepted; stop the service and run v2raya --reset-password"), nil)
	}
	if !common.CheckPassword(password, hash) {
		return "", "", common.Coded("WRONG_CREDENTIALS", fmt.Errorf("wrong username or password"), nil)
	}
	sessionID, refreshToken, err := jwt.CreateSession(username)
	if err != nil {
		return "", "", common.Coded("SESSION_CREATE_FAILED", err, nil)
	}
	token, err = jwt.MakeJWT(map[string]string{
		"uname": username,
		"jti":   sessionID,
	}, durationPtr(jwt.AccessTTL))
	if err != nil {
		_ = jwt.RevokeSession(sessionID)
		return "", "", err
	}
	return token, refreshToken, nil
}

// Refresh rotates a refresh token and issues a new short-lived access token.
func Refresh(refreshToken string) (token, newRefreshToken string, err error) {
	sessionID, username, newRefreshToken, err := jwt.RefreshSession(refreshToken)
	if err != nil {
		return "", "", common.Coded("SESSION_INVALID", err, nil)
	}
	token, err = jwt.MakeJWT(map[string]string{
		"uname": username,
		"jti":   sessionID,
	}, durationPtr(jwt.AccessTTL))
	if err != nil {
		return "", "", err
	}
	return token, newRefreshToken, nil
}

func Logout(sessionID string) error {
	return jwt.RevokeSession(sessionID)
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}

func IsValidAccount(username, password string) bool {
	pwd, err := configure.GetPasswordOfAccount(username)
	if err != nil {
		return false
	}
	return common.CheckPassword(password, pwd)
}

func Register(username, password string) (token, refreshToken string, err error) {
	if configure.ExistsAccount(username) {
		return "", "", common.Coded("USERNAME_TAKEN", fmt.Errorf("username %q is already taken", username), map[string]interface{}{"username": username})
	}
	hash, err := common.CryptoPwd(password)
	if err != nil {
		return "", "", common.Coded("PASSWORD_HASH_FAILED", err, nil)
	}
	err = configure.SetAccount(username, hash)
	if err != nil {
		return
	}
	return Login(username, password)
}

func ValidPasswordLength(password string) (bool, error) {
	// This is the administrator of a root-equivalent service, and bcrypt
	// refuses more than 72 bytes. The lower bound applies to new passwords;
	// an old account with a shorter one can still sign in.
	if len([]byte(password)) >= 12 && len([]byte(password)) <= 72 {
		return true, nil
	}
	return false, fmt.Errorf("length of password should be between 12 and 72 bytes")
}

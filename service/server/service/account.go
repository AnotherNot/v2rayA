package service

import (
	"fmt"
	"time"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
)

func Login(username, password string) (token, refreshToken string, err error) {
	if !IsValidAccount(username, password) {
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
	err = configure.SetAccount(username, common.CryptoPwd(password))
	if err != nil {
		return
	}
	return Login(username, password)
}

func ValidPasswordLength(password string) (bool, error) {
	if len(password) >= 6 && len(password) <= 32 {
		return true, nil
	} else {
		return false, fmt.Errorf("length of password should be between 6 and 32")
	}
}

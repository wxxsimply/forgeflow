package auth

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool        { return r == RoleAdmin || r == RoleOperator || r == RoleViewer }
func (r Role) CanWriteRuns() bool { return r == RoleAdmin || r == RoleOperator }
func (r Role) CanApprove() bool   { return r == RoleAdmin || r == RoleOperator }

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Role        Role      `json:"role"`
	Status      string    `json:"status"`
	MFAEnabled  bool      `json:"mfaEnabled"`
	MFARequired bool      `json:"mfaRequired"`
	CreatedAt   time.Time `json:"createdAt"`
}

type UserCredential struct {
	User
	PasswordHash               string
	MFASecretCiphertext        []byte
	MFAPendingSecretCiphertext []byte
	MFAPendingExpiresAt        *time.Time
	MFAEnabledAt               *time.Time
	MFARecoveryCodeHashes      []string
	MFALastUsedStep            int64
}

type Session struct {
	ID            string     `json:"id"`
	UserID        string     `json:"-"`
	TokenHash     []byte     `json:"-"`
	CSRFHash      []byte     `json:"-"`
	SourceIP      string     `json:"sourceIp"`
	UserAgent     string     `json:"userAgent"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastSeenAt    time.Time  `json:"lastSeenAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	IdleExpiresAt time.Time  `json:"idleExpiresAt"`
	MFAVerifiedAt *time.Time `json:"mfaVerifiedAt,omitempty"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
}

type Principal struct {
	User    User    `json:"user"`
	Session Session `json:"session"`
}

var ErrNotFound = errors.New("auth record not found")
var ErrEmailExists = errors.New("email is already registered")

type Store interface {
	CountUsers(context.Context) (int, error)
	CreateUser(context.Context, UserCredential) error
	FindUserByEmail(context.Context, string) (UserCredential, error)
	FindUserByID(context.Context, string) (UserCredential, error)
	UpdatePasswordHash(context.Context, string, string) error
	SetMFAPending(context.Context, string, []byte, time.Time) error
	EnableMFA(context.Context, string, string, []byte, []string, int64, time.Time) error
	UseMFATimestep(context.Context, string, int64) (bool, error)
	ConsumeMFARecoveryCode(context.Context, string, string) (bool, error)
	CreateSession(context.Context, Session) error
	FindSessionByTokenHash(context.Context, []byte) (Session, error)
	ListSessions(context.Context, string) ([]Session, error)
	TouchSession(context.Context, string, time.Time, time.Time) error
	RevokeSession(context.Context, string, string) error
	RevokeToken(context.Context, []byte) error
}

func NormalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

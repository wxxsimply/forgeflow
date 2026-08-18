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
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type UserCredential struct {
	User
	PasswordHash string
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
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
}

type Principal struct {
	User    User    `json:"user"`
	Session Session `json:"session"`
}

var ErrNotFound = errors.New("auth record not found")

type Store interface {
	CountUsers(context.Context) (int, error)
	CreateUser(context.Context, UserCredential) error
	FindUserByEmail(context.Context, string) (UserCredential, error)
	FindUserByID(context.Context, string) (UserCredential, error)
	UpdatePasswordHash(context.Context, string, string) error
	CreateSession(context.Context, Session) error
	FindSessionByTokenHash(context.Context, []byte) (Session, error)
	ListSessions(context.Context, string) ([]Session, error)
	TouchSession(context.Context, string, time.Time, time.Time) error
	RevokeSession(context.Context, string, string) error
	RevokeToken(context.Context, []byte) error
}

func NormalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

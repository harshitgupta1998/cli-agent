package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Principal struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

type Token struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type Repository interface {
	EnsureUser(ctx context.Context, principal Principal) error
	AuthenticateToken(ctx context.Context, tokenHash string) (Principal, error)
	CreateToken(ctx context.Context, principal Principal, name string, tokenHash string) (Token, error)
	RevokeToken(ctx context.Context, principal Principal, tokenID string) error
}

type Manager struct {
	repository    Repository
	bootstrapHash []byte
	bootstrap     Principal
}

func NewManager(repository Repository, bootstrapToken string, principal Principal) Manager {
	var hash []byte
	if strings.TrimSpace(bootstrapToken) != "" {
		sum := sha256.Sum256([]byte(bootstrapToken))
		hash = sum[:]
	}
	return Manager{repository: repository, bootstrapHash: hash, bootstrap: principal}
}

func (m Manager) Enabled() bool { return len(m.bootstrapHash) > 0 }

func (m Manager) LocalPrincipal() Principal { return m.bootstrap }

func (m Manager) EnsureBootstrapUser(ctx context.Context) error {
	if m.repository == nil {
		return nil
	}
	return m.repository.EnsureUser(ctx, m.bootstrap)
}

func (m Manager) Authenticate(ctx context.Context, token string) (Principal, bool, error) {
	if strings.TrimSpace(token) == "" {
		return Principal{}, false, nil
	}
	sum := sha256.Sum256([]byte(token))
	if len(m.bootstrapHash) > 0 && subtle.ConstantTimeCompare(sum[:], m.bootstrapHash) == 1 {
		return m.bootstrap, true, nil
	}
	if m.repository == nil {
		return Principal{}, false, nil
	}
	principal, err := m.repository.AuthenticateToken(ctx, hex.EncodeToString(sum[:]))
	if errors.Is(err, ErrNotFound) {
		return Principal{}, false, nil
	}
	return principal, err == nil, err
}

func (m Manager) CreateToken(ctx context.Context, principal Principal, name string) (Token, string, error) {
	if m.repository == nil {
		return Token{}, "", errors.New("token storage is unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Token{}, "", errors.New("token_name_required")
	}
	if len(name) > 128 {
		return Token{}, "", errors.New("token_name_too_long")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return Token{}, "", fmt.Errorf("generate token: %w", err)
	}
	plain := "tmd_" + hex.EncodeToString(secret)
	sum := sha256.Sum256([]byte(plain))
	token, err := m.repository.CreateToken(ctx, principal, name, hex.EncodeToString(sum[:]))
	return token, plain, err
}

func (m Manager) RevokeToken(ctx context.Context, principal Principal, tokenID string) error {
	if m.repository == nil {
		return errors.New("token storage is unavailable")
	}
	return m.repository.RevokeToken(ctx, principal, tokenID)
}

var ErrNotFound = errors.New("auth record not found")

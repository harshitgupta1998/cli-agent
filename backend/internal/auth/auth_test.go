package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

type fakeRepository struct {
	principal Principal
	hash      string
	tokens    map[string]Token
}

func (f *fakeRepository) EnsureUser(context.Context, Principal) error { return nil }

func (f *fakeRepository) AuthenticateToken(_ context.Context, hash string) (Principal, error) {
	if hash != f.hash {
		return Principal{}, ErrNotFound
	}
	return f.principal, nil
}

func (f *fakeRepository) CreateToken(_ context.Context, _ Principal, name, hash string) (Token, error) {
	f.hash = hash
	f.principal = Principal{UserID: "usr_1", Name: "User One"}
	token := Token{ID: "tok_1", Name: name}
	if f.tokens == nil {
		f.tokens = map[string]Token{}
	}
	f.tokens[token.ID] = token
	return token, nil
}

func (f *fakeRepository) RevokeToken(_ context.Context, _ Principal, id string) error {
	delete(f.tokens, id)
	f.hash = ""
	return nil
}

func TestCreateTokenStoresOnlyHashAndAuthenticates(t *testing.T) {
	repository := &fakeRepository{}
	manager := NewManager(repository, "bootstrap-token-at-least-32-characters", Principal{UserID: "usr_1", Name: "User One"})
	token, secret, err := manager.CreateToken(context.Background(), Principal{UserID: "usr_1", Name: "User One"}, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if token.ID == "" || token.Name != "cli" || secret == "" {
		t.Fatalf("unexpected token response: %#v secret_empty=%v", token, secret == "")
	}
	if repository.hash == secret {
		t.Fatal("repository stored the plaintext token")
	}
	sum := sha256.Sum256([]byte(secret))
	if repository.hash != hex.EncodeToString(sum[:]) {
		t.Fatal("repository did not receive the SHA-256 token hash")
	}
	principal, ok, err := manager.Authenticate(context.Background(), secret)
	if err != nil || !ok || principal.UserID != "usr_1" {
		t.Fatalf("Authenticate() = (%#v, %v, %v), want user usr_1", principal, ok, err)
	}
}

func TestBootstrapTokenAndUnknownToken(t *testing.T) {
	manager := NewManager(nil, "bootstrap-token-at-least-32-characters", Principal{UserID: "usr_owner", Name: "Owner"})
	principal, ok, err := manager.Authenticate(context.Background(), "bootstrap-token-at-least-32-characters")
	if err != nil || !ok || principal.UserID != "usr_owner" {
		t.Fatalf("bootstrap auth = (%#v, %v, %v)", principal, ok, err)
	}
	if _, ok, err := manager.Authenticate(context.Background(), "wrong"); err != nil || ok {
		t.Fatalf("unknown token auth = (ok=%v, err=%v), want rejection", ok, err)
	}
}

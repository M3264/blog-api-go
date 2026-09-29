package community

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGoogleOIDCValidationAndExplicitLinking(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var issuer, nonce string
	subject, email, name := "subject-one", "google@example.test", "Google Reader"
	wrongNonce := false
	encode := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "one", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			r.ParseForm()
			if r.FormValue("code_verifier") == "" {
				t.Error("PKCE verifier missing")
			}
			n := nonce
			if wrongNonce {
				n = "wrong"
			}
			payload := encode(map[string]any{"alg": "RS256", "kid": "one"}) + "." + encode(map[string]any{"iss": issuer, "aud": "test-client", "sub": subject, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": n, "email": email, "email_verified": true, "name": name})
			h := sha256.Sum256([]byte(payload))
			signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
			json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access", "token_type": "Bearer", "id_token": payload + "." + base64.RawURLEncoding.EncodeToString(signature)})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	issuer = server.URL
	s.OIDCIssuer = issuer
	s.Config.GoogleID = "test-client"
	s.Config.GoogleSecret = "test-secret"
	start := func(link int64) string {
		t.Helper()
		auth, state, e := s.GoogleStart(ctx, link)
		if e != nil {
			t.Fatal(e)
		}
		u, _ := url.Parse(auth)
		nonce = u.Query().Get("nonce")
		if u.Query().Get("code_challenge") == "" {
			t.Fatal("PKCE challenge missing")
		}
		return state
	}
	state := start(0)
	id, e := s.GoogleFinish(ctx, state, "ok")
	if e != nil || id == 0 {
		t.Fatal(e)
	}
	if _, e = s.GoogleFinish(ctx, state, "ok"); e == nil {
		t.Fatal("OAuth state reused")
	}
	state = start(0)
	again, e := s.GoogleFinish(ctx, state, "ok")
	if e != nil || again != id {
		t.Fatal("linked identity did not log in")
	}
	subject = "subject-two"
	state = start(0)
	if _, e = s.GoogleFinish(ctx, state, "ok"); e == nil || !strings.Contains(e.Error(), "link Google") {
		t.Fatal("Google auto-linked by email")
	}
	var target int64
	s.DB.QueryRow(`INSERT INTO users(email,name,verified) VALUES('password@example.test','Password Reader',true) RETURNING id`).Scan(&target)
	email = "different-google@example.test"
	state = start(target)
	linked, e := s.GoogleFinish(ctx, state, "ok")
	if e != nil || linked != target {
		t.Fatal("authenticated Google linking failed", e)
	}
	state = start(0)
	wrongNonce = true
	if _, e = s.GoogleFinish(ctx, state, "ok"); e == nil {
		t.Fatal("bad nonce accepted")
	}
	wrongNonce = false
	s.DB.Exec(`UPDATE users SET suspended=true WHERE id=$1`, target)
	state = start(0)
	if _, e = s.GoogleFinish(ctx, state, "ok"); e == nil {
		t.Fatal("suspended Google account logged in")
	}
}

package community

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/argon2"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	URL, ResendKey, EmailFrom, GoogleID, GoogleSecret, MediaDir string
	SMTPAddr, SMTPMode, SMTPUser, SMTPPassword                  string
	Secure                                                      bool
	Legacy                                                      bool
}
type Service struct {
	DB            *sql.DB
	Config        Config
	Redis         *redis.Client
	EmailHTTP     *http.Client
	EmailEndpoint string
	OIDCIssuer    string
}
type User struct {
	ID        int64  `json:"id"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Verified  bool   `json:"verified"`
	Suspended bool   `json:"suspended"`
	Bio       string `json:"bio"`
	CSRF      string `json:"csrf,omitempty"`
}

func Random() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Hash(t string) string { b := sha256.Sum256([]byte(t)); return hex.EncodeToString(b[:]) }
func Password(p string) (string, error) {
	if len(p) < 12 || len(p) > 256 {
		return "", errors.New("use a password of 12 to 256 characters")
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key := argon2.IDKey([]byte(p), salt, 2, 64*1024, 2, 32)
	return "$argon2id$v=19$m=65536,t=2,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func CheckPassword(encoded, p string) bool {
	if len(p) > 256 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[3] != "m=65536,t=2,p=2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, 2, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}
func (s *Service) User(ctx context.Context, token string) (User, error) {
	var u User
	e := s.DB.QueryRowContext(ctx, `SELECT u.id,u.email,u.name,u.role,u.verified,u.suspended,u.bio,s.csrf FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.hash=$1 AND s.expires_at>now() AND NOT u.suspended`, Hash(token)).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Verified, &u.Suspended, &u.Bio, &u.CSRF)
	return u, e
}
func (s *Service) Session(ctx context.Context, id int64) (string, error) {
	t := Random()
	_, e := s.DB.ExecContext(ctx, `INSERT INTO sessions(hash,user_id,csrf,expires_at) VALUES($1,$2,$3,$4)`, Hash(t), id, Random(), time.Now().Add(30*24*time.Hour))
	return t, e
}
func (s *Service) Limited(ctx context.Context, key string, max int) bool {
	key = Hash(key)
	if s.Redis != nil {
		v, e := s.Redis.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],900) end; return n`, []string{"offscript:rate:" + key}).Int()
		if e == nil {
			return v > max
		}
	}
	var n int
	e := s.DB.QueryRowContext(ctx, `INSERT INTO rate_limits(key,count,expires_at) VALUES($1,1,now()+interval '15 minutes') ON CONFLICT(key) DO UPDATE SET count=CASE WHEN rate_limits.expires_at<now() THEN 1 ELSE rate_limits.count+1 END,expires_at=CASE WHEN rate_limits.expires_at<now() THEN now()+interval '15 minutes' ELSE rate_limits.expires_at END RETURNING count`, key).Scan(&n)
	return e != nil || n > max
}
func (s *Service) Token(ctx context.Context, id int64, purpose string) (string, error) {
	t := Random()
	_, e := s.DB.ExecContext(ctx, `INSERT INTO account_tokens(hash,user_id,purpose,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, Hash(t), id, purpose)
	return t, e
}
func (s *Service) QueueAccount(ctx context.Context, id int64, email, purpose string) error {
	t, e := s.Token(ctx, id, purpose)
	if e != nil {
		return e
	}
	link := s.Config.URL + "/account/" + purpose + "?token=" + t
	_, e = s.DB.ExecContext(ctx, `INSERT INTO email_outbox(user_id,recipient,subject,html,event_key) VALUES($1,$2,$3,$4,$5)`, id, email, "Offscript · "+purpose, `<p>Complete your Offscript account request:</p><p><a href="`+link+`">Continue</a></p><p>This link expires in one hour.</p>`, purpose+":"+Hash(t))
	return e
}
func (s *Service) Bootstrap(ctx context.Context, email string) (string, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(32640002)`); e != nil {
		return "", e
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&n); e != nil {
		return "", e
	}
	if n != 0 {
		return "", errors.New("an admin already exists; use an authenticated invitation")
	}
	var id int64
	e = tx.QueryRowContext(ctx, `INSERT INTO users(email,name,role) VALUES($1,'Editor','admin') RETURNING id`, strings.ToLower(email)).Scan(&id)
	if e != nil {
		return "", e
	}
	t := Random()
	_, e = tx.ExecContext(ctx, `INSERT INTO account_tokens(hash,user_id,purpose,expires_at) VALUES($1,$2,'setup',now()+interval '1 hour')`, Hash(t), id)
	if e != nil {
		return "", e
	}
	link := s.Config.URL + "/account/setup?token=" + t
	_, e = tx.ExecContext(ctx, `INSERT INTO email_outbox(user_id,recipient,subject,html,event_key) VALUES($1,$2,$3,$4,$5)`, id, strings.ToLower(email), "Offscript · Set up your admin account", `<p>Your Offscript admin account is ready to set up.</p><p><a href="`+link+`">Choose your password</a></p><p>This link expires in one hour.</p>`, "setup:"+Hash(t))
	if e != nil {
		return "", e
	}
	return link, tx.Commit()
}
func (s *Service) Consume(ctx context.Context, token, purpose, password string) (int64, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var id int64
	e = tx.QueryRowContext(ctx, `DELETE FROM account_tokens WHERE hash=$1 AND purpose=$2 AND expires_at>now() RETURNING user_id`, Hash(token), purpose).Scan(&id)
	if e != nil {
		return 0, errors.New("this link is invalid or expired")
	}
	if purpose == "verify" {
		_, e = tx.ExecContext(ctx, `UPDATE users SET verified=true WHERE id=$1 AND NOT suspended`, id)
	} else {
		var hash string
		hash, e = Password(password)
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE users SET password_hash=$2,verified=true WHERE id=$1 AND NOT suspended`, id, hash)
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
		}
	}
	if e != nil {
		return 0, e
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM account_tokens WHERE user_id=$1 AND purpose=$2`, id, purpose)
	if e != nil {
		return 0, e
	}
	return id, tx.Commit()
}
func (s *Service) Member(ctx context.Context, id int64, role string, suspended bool) error {
	if role != "reader" && role != "admin" {
		return errors.New("invalid role")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(32640002)`); e != nil {
		return e
	}
	var current string
	var active bool
	e = tx.QueryRowContext(ctx, `SELECT role,NOT suspended FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&current, &active)
	if e != nil {
		return e
	}
	if current == "admin" && active && (role != "admin" || suspended) {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role='admin' AND NOT suspended AND (verified OR id=$1)`, id).Scan(&n); e != nil {
			return e
		}
		if n <= 1 {
			return errors.New("the last active admin must remain active")
		}
	}
	_, e = tx.ExecContext(ctx, `UPDATE users SET role=$2,suspended=$3 WHERE id=$1`, id, role, suspended)
	if e != nil {
		return e
	}
	if suspended {
		_, e = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

package community

import (
	"context"
	"database/sql"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"time"
)

func (s *Service) Google(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
	if s.Config.GoogleID == "" || s.Config.GoogleSecret == "" {
		return nil, nil, errors.New("Google sign-in has not been configured")
	}
	issuer := s.OIDCIssuer
	if issuer == "" {
		issuer = "https://accounts.google.com"
	}
	p, e := oidc.NewProvider(ctx, issuer)
	if e != nil {
		return nil, nil, e
	}
	return p, &oauth2.Config{ClientID: s.Config.GoogleID, ClientSecret: s.Config.GoogleSecret, RedirectURL: s.Config.URL + "/auth/google/callback", Endpoint: p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}, nil
}
func (s *Service) GoogleStart(ctx context.Context, link int64) (string, string, error) {
	_, c, e := s.Google(ctx)
	if e != nil {
		return "", "", e
	}
	state, nonce, verifier := Random(), Random(), oauth2.GenerateVerifier()
	var uid any
	if link != 0 {
		uid = link
	}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO oauth_states(hash,nonce,verifier,link_user,expires_at) VALUES($1,$2,$3,$4,$5)`, Hash(state), nonce, verifier, uid, time.Now().Add(10*time.Minute))
	return c.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), state, e
}
func (s *Service) GoogleFinish(ctx context.Context, state, code string) (int64, error) {
	var nonce, verifier string
	var link sql.NullInt64
	e := s.DB.QueryRowContext(ctx, `DELETE FROM oauth_states WHERE hash=$1 AND expires_at>now() RETURNING nonce,verifier,link_user`, Hash(state)).Scan(&nonce, &verifier, &link)
	if e != nil {
		return 0, errors.New("Google sign-in expired; try again")
	}
	p, c, e := s.Google(ctx)
	if e != nil {
		return 0, e
	}
	token, e := c.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if e != nil {
		return 0, errors.New("Google exchange failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return 0, errors.New("missing Google identity")
	}
	identity, e := p.Verifier(&oidc.Config{ClientID: c.ClientID}).Verify(ctx, raw)
	if e != nil || identity.Nonce != nonce {
		return 0, errors.New("invalid Google identity")
	}
	var claims struct {
		Email, Name string
		Verified    bool `json:"email_verified"`
	}
	if e = identity.Claims(&claims); e != nil || !claims.Verified {
		return 0, errors.New("Google email must be verified")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var id int64
	e = tx.QueryRowContext(ctx, `SELECT i.user_id FROM identities i JOIN users u ON u.id=i.user_id WHERE provider='google' AND subject=$1 AND NOT u.suspended`, identity.Subject).Scan(&id)
	if e == nil {
		if link.Valid && link.Int64 != id {
			return 0, errors.New("this Google identity belongs to another account")
		}
		return id, tx.Commit()
	}
	if e != sql.ErrNoRows {
		return 0, e
	}
	if link.Valid {
		id = link.Int64
		var active bool
		if e = tx.QueryRowContext(ctx, `SELECT NOT suspended FROM users WHERE id=$1`, id).Scan(&active); e != nil || !active {
			return 0, errors.New("account unavailable")
		}
	} else {
		var exists bool
		tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, claims.Email).Scan(&exists)
		if exists {
			return 0, errors.New("sign in with your password, then link Google from account settings")
		}
		e = tx.QueryRowContext(ctx, `INSERT INTO users(email,name,verified) VALUES(lower($1),$2,true) RETURNING id`, claims.Email, claims.Name).Scan(&id)
		if e != nil {
			return 0, e
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO identities(provider,subject,user_id) VALUES('google',$1,$2)`, identity.Subject, id)
	if e != nil {
		return 0, errors.New("Google is already linked")
	}
	return id, tx.Commit()
}

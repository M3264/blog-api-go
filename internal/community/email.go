package community

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type emailMessage struct {
	Recipient, Subject, HTML, Key string
	Headers                       map[string]string
}

func (s *Service) emailReady() bool {
	return s.Config.EmailFrom != "" && (s.Config.SMTPAddr != "" || s.Config.ResendKey != "")
}
func (s *Service) sendEmail(ctx context.Context, msg emailMessage) error {
	if s.Config.SMTPAddr != "" {
		return s.sendSMTP(ctx, msg)
	}
	return s.sendResend(ctx, msg)
}
func (s *Service) sendResend(ctx context.Context, msg emailMessage) error {
	payload := map[string]any{"from": s.Config.EmailFrom, "to": []string{msg.Recipient}, "subject": msg.Subject, "html": msg.HTML}
	if len(msg.Headers) > 0 {
		payload["headers"] = msg.Headers
	}
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", s.emailEndpoint(), bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+s.Config.ResendKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", Hash(msg.Key))
	resp, e := s.emailClient().Do(req)
	if e != nil {
		return errors.New("delivery request failed")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider HTTP %d", resp.StatusCode)
	}
	return nil
}
func (s *Service) sendSMTP(ctx context.Context, msg emailMessage) error {
	from, e := mail.ParseAddress(s.Config.EmailFrom)
	if e != nil {
		return errors.New("invalid email sender")
	}
	to, e := mail.ParseAddress(msg.Recipient)
	if e != nil {
		return errors.New("invalid email recipient")
	}
	host, _, e := net.SplitHostPort(s.Config.SMTPAddr)
	if e != nil {
		return errors.New("SMTP address must be host:port")
	}
	mode := s.Config.SMTPMode
	if mode == "" {
		mode = "starttls"
	}
	if mode != "starttls" && mode != "tls" && mode != "local" {
		return errors.New("invalid SMTP TLS mode")
	}
	if mode == "local" && host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return errors.New("unencrypted SMTP is restricted to loopback relays")
	}
	for _, value := range []string{s.Config.EmailFrom, msg.Recipient, msg.Subject} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("invalid email header")
		}
	}
	for _, name := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if strings.ContainsAny(msg.Headers[name], "\r\n") {
			return errors.New("invalid newsletter header")
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if mode == "tls" {
		conn, e = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", s.Config.SMTPAddr)
	} else {
		conn, e = dialer.DialContext(ctx, "tcp", s.Config.SMTPAddr)
	}
	if e != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	client, e := smtp.NewClient(conn, host)
	if e != nil {
		return errors.New("SMTP greeting failed")
	}
	defer client.Close()
	if mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP relay does not support required STARTTLS")
		}
		if e = client.StartTLS(tlsConfig); e != nil {
			return errors.New("SMTP TLS negotiation failed")
		}
	}
	if s.Config.SMTPUser != "" {
		if e = client.Auth(smtp.PlainAuth("", s.Config.SMTPUser, s.Config.SMTPPassword, host)); e != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if e = client.Mail(from.Address); e != nil {
		return errors.New("SMTP sender rejected")
	}
	if e = client.Rcpt(to.Address); e != nil {
		return errors.New("SMTP recipient rejected")
	}
	data, e := client.Data()
	if e != nil {
		return errors.New("SMTP message rejected")
	}
	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n", from.String(), to.String(), mime.QEncoding.Encode("UTF-8", msg.Subject), time.Now().UTC().Format(time.RFC1123Z))
	domain := "offscript.local"
	if u, e := url.Parse(s.Config.URL); e == nil && u.Hostname() != "" {
		domain = u.Hostname()
	}
	fmt.Fprintf(&message, "Message-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n", Hash(msg.Key), domain)
	for _, name := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		value := msg.Headers[name]
		if value != "" {
			fmt.Fprintf(&message, "%s: %s\r\n", name, value)
		}
	}
	message.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(msg.HTML))
	for len(encoded) > 76 {
		message.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	message.WriteString(encoded + "\r\n")
	if _, e = data.Write(message.Bytes()); e != nil {
		return errors.New("SMTP message transfer failed")
	}
	if e = data.Close(); e != nil {
		return errors.New("SMTP delivery not accepted")
	}
	// DATA acceptance is the success boundary. A failed QUIT must not resend a message.
	_ = client.Quit()
	return nil
}

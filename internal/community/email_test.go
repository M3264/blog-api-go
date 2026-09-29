package community

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// A real loopback SMTP connection exercises acceptance, MIME and retry boundaries.
func smtpFixture(t *testing.T, replies ...int) (string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan string, len(replies)+1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, code := range replies {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			reader := bufio.NewReader(conn)
			fmt.Fprint(conn, "220 example.test SMTP\r\n")
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				switch {
				case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
					fmt.Fprint(conn, "250-example.test\r\n250 SIZE 1000000\r\n")
				case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
					fmt.Fprint(conn, "250 OK\r\n")
				case strings.HasPrefix(line, "DATA"):
					fmt.Fprint(conn, "354 Send data\r\n")
					var body strings.Builder
					for {
						v, e := reader.ReadString('\n')
						if e != nil || v == ".\r\n" {
							break
						}
						body.WriteString(v)
					}
					messages <- body.String()
					fmt.Fprintf(conn, "%d Message response\r\n", code)
				case strings.HasPrefix(line, "QUIT"):
					fmt.Fprint(conn, "221 Bye\r\n")
					conn.Close()
				default:
					fmt.Fprint(conn, "500 Unsupported\r\n")
				}
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { ln.Close(); <-done })
	return ln.Addr().String(), messages
}
func TestSMTPMessageAndRequiredTLS(t *testing.T) {
	address, messages := smtpFixture(t, 250, 250)
	s := &Service{Config: Config{URL: "https://example.test", EmailFrom: "Offscript <sender@example.test>", SMTPAddr: address, SMTPMode: "local"}}
	msg := emailMessage{Recipient: "reader@example.test", Subject: "A story — today", HTML: "<p>Hello &amp; welcome</p>", Key: "publication-one", Headers: map[string]string{"List-Unsubscribe": "<https://example.test/unsubscribe/test>", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}}
	var firstID string
	for i := 0; i < 2; i++ {
		if err := s.sendEmail(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
		parsed, err := mail.ReadMessage(strings.NewReader(<-messages))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(parsed.Body)
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.ReplaceAll(string(body), "\r", ""), "\n", ""))
		if err != nil || string(decoded) != msg.HTML {
			t.Fatal("invalid SMTP MIME payload")
		}
		if parsed.Header.Get("List-Unsubscribe") == "" {
			t.Fatal("unsubscribe header missing")
		}
		id := parsed.Header.Get("Message-ID")
		if i == 0 {
			firstID = id
		}
		if id == "" || id != firstID {
			t.Fatal("retry message ID changed")
		}
	}
	msg.Subject = "unsafe\r\nBcc: victim@example.test"
	if err := s.sendEmail(context.Background(), msg); err == nil {
		t.Fatal("header injection accepted")
	}
	msg.Subject = "Safe subject"
	msg.Headers["List-Unsubscribe"] = "unsafe\r\nBcc: victim@example.test"
	if err := s.sendEmail(context.Background(), msg); err == nil {
		t.Fatal("newsletter header injection accepted")
	}
	delete(msg.Headers, "List-Unsubscribe")
	s.Config.SMTPAddr = "external.example.test:25"
	if err := s.sendEmail(context.Background(), emailMessage{Recipient: "reader@example.test"}); err == nil {
		t.Fatal("unencrypted remote SMTP accepted")
	}
	address, _ = smtpFixture(t, 250)
	s.Config.SMTPAddr = address
	s.Config.SMTPMode = "starttls"
	msg.Subject = "Test"
	if err := s.sendEmail(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatal("TLS downgrade allowed", err)
	}
}
func TestSMTPOutboxRetryAndAcceptance(t *testing.T) {
	s := service(t)
	address, messages := smtpFixture(t, 450, 250)
	s.Config.SMTPAddr = address
	s.Config.SMTPMode = "local"
	s.Config.EmailFrom = "sender@example.test"
	_, err := s.DB.Exec(`INSERT INTO email_outbox(recipient,subject,html,event_key,kind) VALUES('reader@example.test','Test','<p>hello</p>','smtp-retry','verification')`)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"pending", "sent"} {
		if err := s.deliver(context.Background()); err != nil {
			t.Fatal(err)
		}
		var state string
		var attempts int
		if err := s.DB.QueryRow(`SELECT state,attempts FROM email_outbox WHERE event_key='smtp-retry'`).Scan(&state, &attempts); err != nil {
			t.Fatal(err)
		}
		if state != want || attempts != i+1 {
			t.Fatalf("outbox: %s %d", state, attempts)
		}
		if _, err := s.DB.Exec(`UPDATE email_outbox SET available_at=now()`); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := mail.ReadMessage(strings.NewReader(<-messages))
	second, _ := mail.ReadMessage(strings.NewReader(<-messages))
	if first.Header.Get("Message-ID") != second.Header.Get("Message-ID") {
		t.Fatal("outbox retry changed message ID")
	}
	if err := s.deliver(context.Background()); err != nil {
		t.Fatal(err)
	}
	var attempts int
	s.DB.QueryRow(`SELECT attempts FROM email_outbox WHERE event_key='smtp-retry'`).Scan(&attempts)
	if attempts != 2 {
		t.Fatal("accepted message resent")
	}
}

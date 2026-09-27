package registration

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type SMTPSender struct {
	host, from, user, password string
	port                       int
}

func NewSMTPSender(host string, port int, from, user, password string) (*SMTPSender, error) {
	host = strings.TrimSpace(host)
	from = strings.TrimSpace(from)
	address, err := mail.ParseAddress(from)
	if host == "" || (port != 465 && port != 587) || err != nil || address.Address != from ||
		strings.TrimSpace(user) == "" || password == "" || strings.ContainsAny(host+from+user, "\r\n\x00") {
		return nil, fmt.Errorf("smtp registration sender configuration is invalid")
	}
	return &SMTPSender{host: host, port: port, from: from, user: user, password: password}, nil
}

func (s *SMTPSender) Send(ctx context.Context, recipient, code string) error {
	address := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	if s.port == 465 {
		secure := tls.Client(connection, &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		connection = secure
	}
	client, err := smtp.NewClient(connection, s.host)
	if err != nil {
		return err
	}
	defer client.Close()
	if s.port == 587 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if err := client.Auth(smtp.PlainAuth("", s.user, s.password, s.host)); err != nil {
		return err
	}
	if err := client.Mail(s.from); err != nil {
		return err
	}
	if err := client.Rcpt(recipient); err != nil {
		return err
	}
	message, err := client.Data()
	if err != nil {
		return err
	}
	content := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: ForgeFlow registration code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour ForgeFlow registration code is %s. It expires in 10 minutes.\r\nIf you did not request this, ignore this email.\r\n", s.from, recipient, code)
	if _, err := io.WriteString(message, content); err != nil {
		_ = message.Close()
		return err
	}
	if err := message.Close(); err != nil {
		return err
	}
	return client.Quit()
}

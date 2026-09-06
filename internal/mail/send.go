package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	stdmail "net/mail"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
	gomail "github.com/wneessen/go-mail"
)

const SendTimeout = 15 * time.Second

var (
	ErrDisabled  = errors.New("mail is not configured")
	ErrDelivery  = errors.New("mail delivery failed")
	ErrRecipient = errors.New("mail recipient must be one RFC 5322 address")
	ErrSubject   = errors.New("mail subject must not contain line breaks")
)

// Send opens one verified TLS connection with an absolute operation deadline.
// Errors deliberately omit SMTP responses, message bodies, and credentials.
func (c *Client) Send(ctx context.Context, to, subject, body string) error {
	if !c.Configured() {
		return ErrDisabled
	}
	if _, err := stdmail.ParseAddress(to); err != nil || strings.ContainsAny(to, "\r\n") {
		return ErrRecipient
	}
	if strings.ContainsAny(subject, "\r\n") {
		return ErrSubject
	}
	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	public, err := netpolicy.NewPublicDialer(c.allowedCIDRs, net.DefaultResolver, &net.Dialer{})
	if err != nil {
		return ErrDelivery
	}
	var connection net.Conn
	var stopCancellation func() bool
	defer func() {
		if stopCancellation != nil {
			stopCancellation()
		}
		if connection != nil {
			_ = connection.Close()
		}
	}()
	dial := func(dialCtx context.Context, network, address string) (net.Conn, error) {
		raw, err := public.DialContext(dialCtx, network, address)
		if err != nil {
			return nil, err
		}
		connection = raw
		stopCancellation = context.AfterFunc(ctx, func() { _ = raw.Close() })
		bounded := &deadlineConn{Conn: raw, deadline: deadline}
		if err := bounded.SetDeadline(deadline); err != nil {
			return nil, err
		}
		if c.cfg.TLS == "implicit" {
			secured := tls.Client(bounded, c.tlsConfig.Clone())
			if err := secured.HandshakeContext(ctx); err != nil {
				return nil, err
			}
			return secured, nil
		}
		return bounded, nil
	}
	opts := []gomail.Option{gomail.WithPort(c.port), gomail.WithTimeout(SendTimeout),
		gomail.WithTLSConfig(c.tlsConfig.Clone()), gomail.WithTLSPolicy(gomail.TLSMandatory), gomail.WithDialContextFunc(dial)}
	if c.cfg.TLS == "implicit" {
		opts = append(opts, gomail.WithSSL())
	}
	if c.cfg.EHLO != "" {
		opts = append(opts, gomail.WithHELO(c.cfg.EHLO))
	}
	if c.cfg.User != "" {
		// Explicit PLAIN remains TLS-bound in go-mail's auth implementation.
		// Its auto-discovery flag misses TLS established by a custom dialer.
		opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthPlain), gomail.WithUsername(c.cfg.User), gomail.WithPassword(c.cfg.Password))
	}
	client, err := gomail.NewClient(c.host, opts...)
	if err != nil {
		return ErrDelivery
	}
	message := gomail.NewMsg()
	if err := message.From(c.cfg.From); err != nil {
		return ErrDelivery
	}
	if err := message.To(to); err != nil {
		return ErrRecipient
	}
	message.Subject(subject)
	message.SetBodyString(gomail.TypeTextPlain, body)
	if err := client.DialAndSendWithContext(ctx, message); err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrDelivery
	}
	return nil
}

// go-mail refreshes socket deadlines per SMTP command. Clamp each refresh so
// a slow peer cannot extend one send past its original deadline.
type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (c *deadlineConn) bound(deadline time.Time) time.Time {
	if deadline.IsZero() || deadline.After(c.deadline) {
		return c.deadline
	}
	return deadline
}
func (c *deadlineConn) SetDeadline(t time.Time) error     { return c.Conn.SetDeadline(c.bound(t)) }
func (c *deadlineConn) SetReadDeadline(t time.Time) error { return c.Conn.SetReadDeadline(c.bound(t)) }
func (c *deadlineConn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(c.bound(t))
}

package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// emailDialTimeout and emailSessionTimeout bound connection setup and the complete SMTP session.
// net/smtp has no built-in timeout; a stalled peer would otherwise block delivery forever and halt the
// single-goroutine dispatcher.
const (
	emailDialTimeout    = 10 * time.Second
	emailSessionTimeout = 45 * time.Second
)

// emailChannel implements SMTP delivery.
type emailChannel struct{}

func (emailChannel) Kind() string { return KindEmail }

// Email has no platform limit, but a generous default prevents notification flooding.
func (emailChannel) DefaultRatePerMin() int { return 60 }

// Only the password is masked. SMTP hosts, usernames, and recipients are not treated as secrets;
// masking them would hinder editing.
func (emailChannel) SecretKeys() []string { return []string{"password"} }

// host/port determine the password's recipient; tls controls encryption. Changing any of them requires
// an explicit password choice, including when disabling TLS.
func (emailChannel) DestinationKeys() []string { return []string{"host", "port", "tls"} }

func (emailChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "host") == "" {
		return locale.NewError("SMTP server address is required")
	}
	port := cfgInt(cfg, "port")
	if port <= 0 || port > 65535 {
		return locale.NewError("Invalid SMTP port (must be 1-65535)")
	}
	if cfgString(cfg, "from") == "" {
		return locale.NewError("Sender address is required")
	}
	if len(cfgStrings(cfg, "to")) == 0 {
		return locale.NewError("At least one recipient address is required")
	}
	return nil
}

func (c emailChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	m = m.withContextLanguage(ctx)
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	host := cfgString(cfg, "host")
	port := cfgInt(cfg, "port")
	from := cfgString(cfg, "from")
	to := cfgStrings(cfg, "to")
	username := cfgString(cfg, "username")
	password := cfgString(cfg, "password")
	implicitTLS := cfgBool(cfg, "tls")

	msg, err := buildEmailMessage(from, to, m)
	if err != nil {
		return 0, Permanent(err)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := emailDial(ctx, addr, host, implicitTLS)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	// Upgrade with STARTTLS when supported. Never send credentials over plaintext; see authentication
	// handling below.
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return 0, locale.Errorf("STARTTLS failed: %w", err)
			}
		}
	}
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			// smtp.PlainAuth correctly refuses credentials on unencrypted connections except localhost. Preserve
			// that protection and explain how to fix the configuration instead of exposing only an opaque
			// encryption error.
			if strings.Contains(err.Error(), "unencrypted connection") {
				return 0, Permanent(locale.Errorf("Credentials withheld: the connection is unencrypted. Enable TLS or use port 465 (implicit TLS) (%w)", err))
			}
			return 0, Permanent(locale.Errorf("SMTP authentication failed: %w", err))
		}
	}
	if err := client.Mail(from); err != nil {
		return 0, smtpEnvelopeError("Sender %s was rejected: %w", from, err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return 0, smtpEnvelopeError("Recipient %s was rejected: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return 0, locale.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return 0, locale.Errorf("Failed to write email body: %w", err)
	}
	if err := w.Close(); err != nil {
		return 0, locale.Errorf("Failed to submit email: %w", err)
	}
	// A Quit failure does not undo server acceptance, so ignore it.
	_ = client.Quit()
	// Email sends the entire HTML body without truncation, so the whole batch is delivered.
	return len(m.Items), nil
}

// emailDial opens SMTP connections. implicitTLS uses immediate TLS, as on port 465; other connections
// start plaintext and may upgrade with STARTTLS on 25/587. Plaintext greetings to 465 fail. Set
// session deadlines before handing the connection to net/smtp, whose underlying connection is private;
// this also bounds the handshake. Control shares HTTP's blockInternalDial guard. Without it,
// localhost/metadata targets allow SSRF, leaked greeting lines through last_error/history, and timing-
// based port probing. Dial-time checks also prevent DNS rebinding.
func emailDial(ctx context.Context, addr, host string, implicitTLS bool) (*smtp.Client, error) {
	d := &net.Dialer{Timeout: emailDialTimeout, Control: blockInternalDial}
	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, locale.Errorf("Failed to connect to SMTP server: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(emailSessionTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, locale.Errorf("SMTP handshake failed: %w", err)
	}
	return client, nil
}

// smtpStageError distinguishes retryable 4xx replies (450 greylisting, 451 local errors, 452 storage
// exhaustion) from permanent 5xx replies (550 unknown recipient, 553 invalid address). Treating every
// reply as permanent defeats greylisting retries. Read the leading three-digit code; unknown codes
// remain retryable rather than discarding potentially transient failures.
func smtpStageError(what string, err error) error {
	code := smtpReplyCode(err.Error())
	if code >= 500 && code < 600 {
		return Permanent(locale.Errorf("%s: %w", what, err))
	}
	return locale.Errorf("%s: %w", what, err)
}

// smtpEnvelopeError retains the built-in template and raw address for localization.
// Classification still depends only on the original SMTP reply code.
func smtpEnvelopeError(template, address string, err error) error {
	wrapped := locale.Errorf(template, address, err)
	code := smtpReplyCode(err.Error())
	if code >= 500 && code < 600 {
		return Permanent(wrapped)
	}
	return wrapped
}

// smtpReplyCode extracts the leading three-digit code, or zero if absent. net/smtp exposes only text
// such as "450 4.7.1 ...", not a structured code.
func smtpReplyCode(text string) int {
	if len(text) < 3 {
		return 0
	}
	n, err := strconv.Atoi(text[:3])
	if err != nil {
		return 0
	}
	return n
}

// buildEmailMessage builds an RFC 5322 message. Base64 avoids HTML lines exceeding SMTP's 1000-byte
// limit and never starts lines with a dot, avoiding dot-escaping concerns.
func buildEmailMessage(from string, to []string, m Message) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	// Encode non-ASCII subjects with RFC 2047 to prevent mojibake.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", htmlTitle(m)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	// Email has no hard body-length limit, so retain the entire body.
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(htmlBody(m, 0)))
	// Wrap base64 at 76 characters per RFC 2045.
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return b.String(), nil
}

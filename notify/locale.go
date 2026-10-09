package notify

import (
	"context"
	"errors"
	"github.com/sebastian93921/artifex/locale"
	"net/url"
)

// withContextLanguage gives an explicit send context priority over message metadata.
func (m Message) withContextLanguage(ctx context.Context) Message {
	if lang, ok := locale.Explicit(ctx); ok {
		m.Language = lang
	}
	m.Language = locale.Resolve(m.Language)
	return m
}

// ErrorMessage preserves retry markers and renders only known built-in errors.
// Unknown provider errors and their raw response content remain unchanged.
func ErrorMessage(lang locale.Lang, err error) string {
	if err == nil {
		return ""
	}
	if e, ok := err.(*PermanentError); ok {
		return ErrorMessage(lang, e.Err)
	}
	var changed *ErrDestinationChangedWithoutCredentials
	if errors.As(err, &changed) {
		return changed.Message(lang)
	}
	return locale.ErrorMessage(lang, err)
}

// redactedTargetArgument preserves a typed placeholder until the display boundary.
func redactedTargetArgument(raw string) any {
	target := redactRequestTarget(raw)
	if target == "(Unparseable URL)" {
		return locale.NewError("(Unparseable URL)")
	}
	return target
}

// redactedTransportArgument keeps the built-in unknown-cause fallback localizable.
// Real driver causes remain redacted verbatim instead of being pattern-matched.
func redactedTransportArgument(err error) any {
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Err == nil {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		return locale.Errorf("%s %s: unknown error", uerr.Op, host)
	}
	return redactTransportError(err)
}

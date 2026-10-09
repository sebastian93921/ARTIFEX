package server

import (
	"archive/zip"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sebastian93921/artifex/locale"
)

func TestRecordsMentionProtocolAndLocalizedErrors(t *testing.T) {
	// Chinese type labels are stable wire tokens, not UI prose to rewrite.
	refs, err := parseChatMentions("@[漏洞#12 title] @[企业#8 name]")
	if err != nil || len(refs) != 2 || refs[0].Kind != "finding" || refs[1].Kind != "company" {
		t.Fatalf("protocol changed: %+v %v", refs, err)
	}
	_, err = parseChatMentions("@[漏洞#0]")
	var input *chatMentionInputError
	if !errors.As(err, &input) || !strings.Contains(locale.ErrorMessage(locale.En, err), "reference ID") {
		t.Fatalf("validation error=%v", err)
	}
	missing := newChatMentionInputError("Referenced %s #%d does not exist or has the wrong type; remove it and select again", locale.NewError(chatMentionLabel("finding")), 42)
	if got := locale.ErrorMessage(locale.En, missing); !strings.Contains(got, "Finding #42") {
		t.Fatalf("nested record label=%q", got)
	}
	raw := "Untouched user text 원본 100%"
	if got, err := composeChatMentionMessageForLanguage(nil, raw, locale.En); err != nil || got != raw {
		t.Fatalf("plain user message changed: %q %v", got, err)
	}
	bounded := boundChatMentionValueForLanguage(map[string]any{"content": raw, "long": strings.Repeat("x", 16001)}, locale.En).(map[string]any)
	if bounded["content"] != raw || !strings.HasSuffix(bounded["long"].(string), "[Field too long; truncated]") {
		t.Fatal("content or generated truncation marker changed incorrectly")
	}
}

func TestRecordsGoalConstraintValidationLanguages(t *testing.T) {
	m := &Manager{tasks: map[string]*Task{"7": {ID: "7"}}}
	s := &Server{m: m, engine: NewEngine(m)}
	for _, lang := range []locale.Lang{locale.En} {
		for _, tc := range []struct {
			handler http.HandlerFunc
			key     string
		}{{s.addGoal, "Goal text must not be empty"}, {s.addConstraint, "Constraint text must not be empty"}} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/tasks/7/record", strings.NewReader(`{"text":""}`))
			req.SetPathValue("id", "7")
			tc.handler(&localeWriter{rec, lang}, req)
			if rec.Code != 400 || !strings.Contains(rec.Body.String(), locale.Text(lang, tc.key)) {
				t.Fatalf("%s validation=%d %s", lang, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestRecordsZipErrorsLocalizeWithoutChangingNames(t *testing.T) {
	name := "custom 100% 원본/SKILL.md"
	err := checkSkillZipMethods([]skillZipEntry{{f: &zip.File{FileHeader: zip.FileHeader{Method: zipMethodDeflate64}}, name: name}})
	if err == nil {
		t.Fatal("unsupported method accepted")
	}
	for _, lang := range []locale.Lang{locale.En} {
		msg := locale.ErrorMessage(lang, err)
		if !strings.Contains(msg, "Deflate64") || !strings.Contains(msg, name) || !strings.Contains(msg, "zip -r") {
			t.Fatalf("method, path, or repair instruction lost: %q", msg)
		}
	}
	_, err = newSkillZipReader([]byte("not a zip"))
	if err == nil || !strings.Contains(locale.ErrorMessage(locale.En, err), "ZIP format required") {
		t.Fatalf("decode error=%v", err)
	}
}

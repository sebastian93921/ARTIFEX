package server

import (
	"encoding/json"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestConversationListRunningState(t *testing.T) {
	s, _ := newRetestServer(t)
	var ids []int64
	for _, key := range []string{"auto", "reporter", "retester"} {
		c, err := s.m.pg.CreateConversation(key, "runtime status test", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	t.Cleanup(func() {
		s.chatMu.Lock()
		clear(s.chatBusy)
		s.chatMu.Unlock()
		for _, id := range ids {
			_, _ = s.m.pg.Exec(`DELETE FROM conversations WHERE id=$1`, id)
		}
	})
	check := func(want map[int64]bool) {
		t.Helper()
		w := retestRequest(s.pgListConversations, http.MethodGet, 0, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		var body struct {
			Conversations []conversationListItem `json:"conversations"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, item := range body.Conversations {
			if expected, ok := want[item.ID]; ok {
				found++
				if item.Running != expected || item.Title != "runtime status test" {
					t.Fatalf("conversation %d: running=%v want=%v title=%q", item.ID, item.Running, expected, item.Title)
				}
			}
		}
		if found != len(want) {
			t.Fatalf("found %d of %d conversations", found, len(want))
		}
	}
	check(map[int64]bool{ids[0]: false, ids[1]: false, ids[2]: false})
	s.chatMu.Lock()
	s.chatBusy[s.convBusyKey(ids[1])] = true
	s.chatBusy[s.convBusyKey(ids[2])] = true
	s.chatBusy["unrelated-task"] = true
	s.chatMu.Unlock()
	check(map[int64]bool{ids[0]: false, ids[1]: true, ids[2]: true})
	s.chatMu.Lock()
	clear(s.chatBusy)
	s.chatMu.Unlock()
	check(map[int64]bool{ids[0]: false, ids[1]: false, ids[2]: false})
}

func TestConversationLocalizedDefaultsAndRawTitles(t *testing.T) {
	s, _ := newRetestServer(t)
	for _, lang := range []locale.Lang{locale.En} {
		for _, title := range []string{"", "Raw user title"} {
			raw, _ := json.Marshal(map[string]string{"agent_key": db.FindingRetestAgentKey, "title": title})
			req := httptest.NewRequest(http.MethodPost, "/api/conversations?lang="+string(lang), strings.NewReader(string(raw)))
			rec := httptest.NewRecorder()
			withLocale(http.HandlerFunc(s.pgCreateConversation)).ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("Create %s: %d %s", lang, rec.Code, rec.Body)
			}
			var c db.Conversation
			if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.m.pg.DeleteConversation(c.ID) })
			want := title
			if want == "" {
				want = locale.Text(lang, "New conversation")
			}
			if c.Title != want {
				t.Fatalf("%s title=%q want=%q", lang, c.Title, want)
			}
		}
	}
	for _, title := range []string{"", locale.Text(locale.En, "New conversation")} {
		if !isDefaultConversationTitle(title) {
			t.Fatalf("Built-in default not recognized: %q", title)
		}
	}
	if isDefaultConversationTitle("Raw user title") {
		t.Fatal("Custom title treated as a default")
	}
}

func TestConversationCatalogCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, path := range []string{"conversations.go", "side_questions.go", "scheduler.go", "triggers.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "locale" {
				return true
			}
			index := 0
			switch sel.Sel.Name {
			case "Text":
				index = 1
			case "Errorf", "NewError":
			default:
				return true
			}
			lit, ok := call.Args[index].(*ast.BasicLit)
			if !ok {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			seen[key] = true
			en, ok := locale.Lookup(locale.En, key)
			if !ok || en != key {
				t.Errorf("Missing English template %q", key)
			}
			return true
		})
	}
	t.Logf("Verified %d conversation/trigger/side-question templates", len(seen))
}

func TestConversationBroadcastAndLogPreserveRawText(t *testing.T) {
	b := NewBroadcaster()
	ch, unsubscribe := b.Subscribe("raw-task-id")
	defer unsubscribe()
	a := db.Activity{Kind: "text", Worker: "raw-worker", Summary: "New conversation", Detail: "Raw unmodified transcript"}
	b.Publish("raw-task-id", a)
	got := <-ch
	if got.Kind != a.Kind || got.Worker != a.Worker || got.Summary != a.Summary || got.Detail != a.Detail {
		t.Fatalf("Broadcast altered raw activity: %+v", got)
	}
	for _, tc := range []struct{ text, level string }{{"[test] operation failed", "error"}, {"[test] 작업 실패", "error"}, {"[test] retry later", "warn"}, {"[test] 재시도", "warn"}, {"[test] raw transcript", "info"}} {
		line := parseLog(tc.text)
		if line.Level != tc.level || line.Text != tc.text || line.Tag != "test" {
			t.Fatalf("Log language/identity changed: %+v", line)
		}
	}
}

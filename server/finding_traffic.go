package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"unicode/utf8"

	"github.com/sebastian93921/artifex/agent"
	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/evidence"
	"github.com/sebastian93921/artifex/locale"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) evidenceStore() *evidence.Store {
	return evidence.New(s.m.pg, s.m.traffic, filepath.Join(s.m.dir, "evidence"))
}

// Add only new optional properties; preserve edited descriptions, existing
// properties, agent bindings and disabled flags. The one-time flag also keeps
// subsequent user unbinding of the evidence reader intact.
func (s *Server) seedFindingTrafficTools() {
	const flag = "finding_traffic_tools_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	for _, key := range []string{"report_finding", "update_finding_report"} {
		var schema any
		if key == "update_finding_report" {
			schema = s.toolUpdateFindingReport(locale.En).InputSchema()
		} else {
			for _, seed := range agent.BuiltinToolSeeds() {
				if seed.Key == key {
					schema = seed.Schema
					break
				}
			}
		}
		raw, err := json.Marshal(schema)
		if err != nil {
			log.Printf(locale.Text(locale.ServerDefault(), "[evidence] tool schema: %v"), err)
			return
		}
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(raw, &obj); err != nil {
			return
		}
		// BuiltinToolSeeds stores Schema as RawMessage; both forms marshal as JSON.
		var properties map[string]json.RawMessage
		if err = json.Unmarshal(obj["properties"], &properties); err != nil {
			return
		}
		name := "traffic_refs"
		if key == "update_finding_report" {
			name = "evidence_version"
		}
		if len(properties[name]) == 0 {
			return
		}
		_, err = s.m.pg.Exec(`UPDATE tools SET schema=jsonb_set(schema,ARRAY['properties',$2::text],$3::jsonb,true),updated_at=now()
WHERE key=$1 AND system AND NOT(COALESCE(schema->'properties','{}'::jsonb) ? $2)`, key, name, string(properties[name]))
		if err != nil {
			log.Printf(locale.Text(locale.ServerDefault(), "[evidence] upgrade tool %s: %v"), key, err)
			return
		}
	}
	if reporter, _ := s.m.pg.GetAgentByKey("reporter"); reporter != nil {
		if err := s.m.pg.AddAgentToToolBinding("reporter", []string{"get_finding_traffic"}); err != nil {
			return
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

func (s *Server) registerFindingTraffic(mux *http.ServeMux) {
	base := "/api/exploration/findings/{id}/traffic"
	mux.HandleFunc("GET "+base, s.getFindingTraffic)
	mux.HandleFunc("POST "+base, s.bindFindingTraffic)
	mux.HandleFunc("PATCH "+base+"/{binding_id}", s.editFindingTraffic)
	mux.HandleFunc("DELETE "+base+"/{binding_id}", s.editFindingTraffic)
	mux.HandleFunc("PUT "+base+"/order", s.editFindingTraffic)
	mux.HandleFunc("GET "+base+"/{binding_id}", s.getFindingTrafficDetail)
	mux.HandleFunc("GET "+base+"/{binding_id}/body", s.getFindingTrafficBody)
}

func evidenceError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	if errors.Is(err, db.ErrEvidenceConflict) || errors.Is(err, db.ErrTaskArchiveState) {
		status = http.StatusConflict
	}
	if errors.Is(err, db.ErrFindingNotFound) || errors.Is(err, db.ErrEvidenceNotFound) {
		status = http.StatusNotFound
	}
	writeError(w, status, err)
}

func (s *Server) findingTrafficAccess(w http.ResponseWriter, r *http.Request, write bool) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "invalid finding id"))
		return 0, false
	}
	f, err := s.m.pg.GetFinding(id)
	if err != nil {
		writeError(w, 500, err)
		return 0, false
	}
	if f == nil {
		writeErr(w, 404, locale.Text(responseLanguage(w), "finding not found"))
		return 0, false
	}
	if taskID := r.URL.Query().Get("context_task"); taskID != "" {
		task := s.m.ResolveTask(taskID)
		if task == nil {
			writeErr(w, 404, locale.Text(responseLanguage(w), "context task not found"))
			return 0, false
		}
		_, inherited, allowed := findingProvenanceInTask(task, f.TaskID)
		if !allowed {
			writeErr(w, 404, locale.Text(responseLanguage(w), "finding not available in task context"))
			return 0, false
		}
		if write && inherited {
			writeErr(w, 403, locale.Text(responseLanguage(w), "Inherited findings are read-only; edit them in the source task"))
			return 0, false
		}
	}
	return id, true
}

func trafficSummary(in *db.FindingTraffic) *db.FindingTraffic {
	out := *in
	out.Bindings = append([]db.FindingTrafficBinding{}, in.Bindings...)
	for i := range out.Bindings {
		out.Bindings[i].Snapshot.ReqHead = ""
		out.Bindings[i].Snapshot.RespHead = ""
	}
	return &out
}

func (s *Server) getFindingTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := s.findingTrafficAccess(w, r, false)
	if !ok {
		return
	}
	out, err := s.m.pg.GetFindingTraffic(r.Context(), id)
	if err != nil {
		evidenceError(w, err)
		return
	}
	writeJSON(w, 200, trafficSummary(out))
}

func (s *Server) bindFindingTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := s.findingTrafficAccess(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Refs []db.TrafficRef `json:"traffic_refs"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, 400, locale.Text(responseLanguage(w), "invalid body"))
		return
	}
	if len(body.Refs) == 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Select traffic to bind"))
		return
	}
	out, err := s.evidenceStore().Bind(r.Context(), id, body.Refs)
	if err != nil {
		evidenceError(w, err)
		return
	}
	writeJSON(w, 200, trafficSummary(out))
}

func (s *Server) editFindingTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := s.findingTrafficAccess(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Version *int64   `json:"version"`
		Role    *string  `json:"role"`
		Note    *string  `json:"note"`
		Order   []string `json:"binding_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.Version == nil {
		writeErr(w, 400, locale.Text(responseLanguage(w), "version and a valid request body are required"))
		return
	}
	var order []int64
	bindingID := int64(0)
	if r.Method == http.MethodPut {
		if body.Order == nil {
			writeErr(w, 400, locale.Text(responseLanguage(w), "binding_ids is required"))
			return
		}
		order = []int64{}
		for _, raw := range body.Order {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v <= 0 {
				writeErr(w, 400, locale.Text(responseLanguage(w), "invalid binding id"))
				return
			}
			order = append(order, v)
		}
	} else {
		var err error
		bindingID, err = strconv.ParseInt(r.PathValue("binding_id"), 10, 64)
		if err != nil || bindingID <= 0 {
			writeErr(w, 400, locale.Text(responseLanguage(w), "invalid binding id"))
			return
		}
	}
	err := s.m.pg.EditFindingTraffic(r.Context(), id, bindingID, *body.Version, body.Role, body.Note, r.Method == http.MethodDelete, order)
	if err != nil {
		evidenceError(w, err)
		return
	}
	s.getFindingTraffic(w, r)
}

type evidencePreview struct {
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	Total      int64  `json:"total"`
	NextOffset int64  `json:"next_offset"`
	Truncated  bool   `json:"truncated"`
	Binary     bool   `json:"binary"`
}

func readEvidencePreview(store *evidence.Store, snapshot db.TrafficEvidenceSnapshot, side string, offset, length int64, langs ...locale.Lang) (out evidencePreview, err error) {
	if offset < 0 || length < 0 {
		return out, locale.NewError("offset / length must not be negative")
	}
	if length == 0 || length > 8192 {
		length = 8192
	}
	f, total, err := store.OpenBody(snapshot, side)
	if err != nil {
		return out, err
	}
	defer f.Close()
	if offset > total {
		return out, locale.NewError("offset exceeds the body length")
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return out, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, length))
	if err != nil {
		return out, err
	}
	// Leave an incomplete trailing UTF-8 rune for the next page. Explicit byte
	// offsets are still accepted; complete downloads always retain original bytes.
	if offset+int64(len(raw)) < total && len(raw) >= utf8.UTFMax {
		start := len(raw) - 1
		for start > 0 && raw[start]&0xc0 == 0x80 {
			start--
		}
		if !utf8.FullRune(raw[start:]) {
			raw = raw[:start]
		}
	}
	out = evidencePreview{Offset: offset, Total: total, NextOffset: offset + int64(len(raw)), Truncated: offset+int64(len(raw)) < total,
		Binary: bytes.IndexByte(raw, 0) >= 0 || (offset == 0 && !utf8.Valid(raw))}
	if out.Binary {
		out.Content = fmt.Sprintf(locale.Text(locale.First(langs), "[Binary body, %d bytes; download to view]"), total)
	} else {
		out.Content = string(bytes.ToValidUTF8(raw, []byte("�")))
	}
	return out, nil
}

func (s *Server) getFindingTrafficDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := s.findingTrafficAccess(w, r, false)
	if !ok {
		return
	}
	bid, err := strconv.ParseInt(r.PathValue("binding_id"), 10, 64)
	if err != nil || bid <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "invalid binding id"))
		return
	}
	store := s.evidenceStore()
	var result any
	err = store.WithBinding(r.Context(), id, bid, func(b db.FindingTrafficBinding) error {
		req, err := readEvidencePreview(store, b.Snapshot, "request", 0, 8192, responseLanguage(w))
		if err != nil {
			return err
		}
		resp, err := readEvidencePreview(store, b.Snapshot, "response", 0, 8192, responseLanguage(w))
		if err != nil {
			return err
		}
		result = map[string]any{"binding": b, "request": req, "response": resp}
		return nil
	})
	if err != nil {
		evidenceError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) getFindingTrafficBody(w http.ResponseWriter, r *http.Request) {
	id, ok := s.findingTrafficAccess(w, r, false)
	if !ok {
		return
	}
	bid, err := strconv.ParseInt(r.PathValue("binding_id"), 10, 64)
	if err != nil || bid <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "invalid binding id"))
		return
	}
	side := r.URL.Query().Get("side")
	store := s.evidenceStore()
	// Resolve the binding under the evidence lock, then read the blob without it:
	// both paths below are O(body size) and would otherwise stall every evidence
	// write for as long as the client takes to receive the data.
	b, err := store.Binding(r.Context(), id, bid)
	if err != nil {
		evidenceError(w, err)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		f, length, err := store.OpenBody(b.Snapshot, side)
		if err != nil {
			evidenceError(w, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"evidence-%d-%s.bin\"", bid, side))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		_, _ = io.Copy(w, f)
		return
	}
	offset, length := int64(0), int64(8192)
	for name, dst := range map[string]*int64{"offset": &offset, "length": &length} {
		if raw := r.URL.Query().Get(name); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				evidenceError(w, err)
				return
			}
			*dst = v
		}
	}
	preview, err := readEvidencePreview(store, b.Snapshot, side, offset, length, responseLanguage(w))
	if err != nil {
		evidenceError(w, err)
		return
	}
	writeJSON(w, 200, preview)
}

func (s *Server) toolGetFindingTraffic(langs ...locale.Lang) actool.CoreTool {
	lang := findingToolLanguage(langs)
	return roTool("get_finding_traffic", locale.Text(lang, "Read a finding's bound real traffic evidence regardless of the capture setting. finding_id is the independent record ID in report_finding JSON, not the exploration node ID on the first line. Omit binding_id to list bindings and version. An empty list is normal: TCP/non-HTTP or uncaptured findings can still be reported using textual/command evidence; binding is optional. For bindings, read body segments using binding_id, side (request/response), and offset. Pass the version actually read as evidence_version to update_finding_report, whose finding_id still uses the exploration node ID."),
		objSchema(map[string]any{"finding_id": strParam(locale.Text(lang, "Independent finding record ID")), "binding_id": strParam(locale.Text(lang, "Binding ID from the list; omit to return the list")), "side": strParam(locale.Text(lang, "request or response; defaults to response")), "offset": map[string]any{"type": "integer"}, "length": map[string]any{"type": "integer"}}, "finding_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				FindingID      json.RawMessage `json:"finding_id"`
				BindingID      json.RawMessage `json:"binding_id"`
				Side           string          `json:"side"`
				Offset, Length int64
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			id, bid := parseProfileID(a.FindingID), parseProfileID(a.BindingID)
			if err := s.agentFindingTrafficAccess(ctx, id, false); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			if len(a.BindingID) == 0 {
				list, err := s.m.pg.GetFindingTraffic(ctx, id)
				if err != nil {
					return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
				}
				return jsonResult(trafficSummary(list))
			}
			if a.Side == "" {
				a.Side = "response"
			}
			store := s.evidenceStore()
			var result any
			err := store.WithBinding(ctx, id, bid, func(b db.FindingTrafficBinding) error {
				preview, err := readEvidencePreview(store, b.Snapshot, a.Side, a.Offset, a.Length, locale.FromContext(ctx))
				if err != nil {
					return err
				}
				head := b.Snapshot.ReqHead
				if a.Side == "response" {
					head = b.Snapshot.RespHead
				}
				result = map[string]any{"binding_id": strconv.FormatInt(bid, 10), "head": head, "body": preview}
				return nil
			})
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return jsonResult(result)
		})
}

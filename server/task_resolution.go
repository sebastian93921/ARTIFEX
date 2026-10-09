package server

import (
	"github.com/Autumn-27/artex/locale"
	"net/http"

	"github.com/Autumn-27/artex/db"
)

type taskLLMResolution struct {
	ProfileID *int64 `json:"profile_id,omitempty"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	Model     string `json:"model"`
	Source    string `json:"source"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func (s *Server) resolutionFromProfile(p *db.LLMProfile, source string) taskLLMResolution {
	return s.resolutionFromProfileForLanguage(p, source, locale.ServerDefault())
}

// Only built-in availability reasons are localized; profile names and model IDs remain untouched.
func (s *Server) resolutionFromProfileForLanguage(p *db.LLMProfile, source string, lang locale.Lang) taskLLMResolution {
	if p == nil {
		return taskLLMResolution{Source: source, Reason: locale.Text(lang, "LLM profile not found")}
	}
	id := p.ID
	result := taskLLMResolution{
		ProfileID: &id,
		Name:      p.Name,
		Format:    p.Format,
		Model:     p.Model,
		Source:    source,
	}
	// Custom endpoints (Ollama, vLLM, LM Studio, …) legitimately run without keys.
	if p.APIKey == "" && p.BaseURL == "" {
		result.Reason = locale.Text(lang, "LLM profile has no API key")
		return result
	}
	if _, _, ok := s.providerForProfile(p.ID); !ok {
		result.Reason = locale.Text(lang, "LLM profile format or parameters are invalid")
		return result
	}
	result.Available = true
	return result
}

// resolveTaskRoleLLM mirrors taskLLMRuntime.current without creating a provider:
// role Agent binding -> explicit task chain -> active/global environment config.
// Database failures are returned rather than represented as an unavailable model.
func (s *Server) resolveTaskRoleLLM(t *Task, agentKey string) (taskLLMResolution, error) {
	return s.resolveTaskRoleLLMForLanguage(t, agentKey, locale.ServerDefault())
}

func (s *Server) resolveTaskRoleLLMForLanguage(t *Task, agentKey string, lang locale.Lang) (taskLLMResolution, error) {
	if s == nil || s.m == nil || s.m.pg == nil {
		return taskLLMResolution{}, locale.Errorf("database unavailable")
	}
	a, err := s.m.pg.GetAgentByKey(agentKey)
	if err != nil {
		return taskLLMResolution{}, locale.Errorf("load agent %q: %w", agentKey, err)
	}
	if a != nil && a.LLMProfileID != nil {
		p, err := s.m.pg.ProfileByID(*a.LLMProfileID)
		if err != nil {
			return taskLLMResolution{}, locale.Errorf("load agent LLM profile: %w", err)
		}
		if p != nil {
			resolved := s.resolutionFromProfileForLanguage(p, "agent_binding", lang)
			if resolved.Available {
				return resolved, nil
			}
		}
	}
	state := t.llmStateSnapshot()
	if len(state.ProfileIDs) > 0 {
		if state.ActiveID == nil {
			return taskLLMResolution{Source: "task_chain", Reason: locale.Text(lang, "The task LLM profile chain has exhausted its quota")}, nil
		}
		p, err := s.m.pg.ProfileByID(*state.ActiveID)
		if err != nil {
			return taskLLMResolution{}, locale.Errorf("load task LLM profile: %w", err)
		}
		return s.resolutionFromProfileForLanguage(p, "task_chain", lang), nil
	}
	p, err := s.m.pg.ActiveProfile()
	if err != nil {
		return taskLLMResolution{}, locale.Errorf("load global LLM profile: %w", err)
	}
	if p != nil {
		resolved := s.resolutionFromProfileForLanguage(p, "global_profile", lang)
		if resolved.Available {
			return resolved, nil
		}
	}
	s.cfgMu.Lock()
	on, name, cfg := s.llmOn, s.llmProf, s.llmCfg
	s.cfgMu.Unlock()
	if on {
		if name == "" {
			name = locale.Text(lang, "Global configuration")
		}
		return taskLLMResolution{
			Name: name, Format: cfg.Provider(), Model: cfg.Model,
			Source: "environment", Available: true,
		}, nil
	}
	return taskLLMResolution{Source: "global", Reason: locale.Text(lang, "No LLM profile is available")}, nil
}

func (s *Server) taskLLMResolutionHandler(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	roles := map[string]taskLLMResolution{}
	for responseKey, agentKey := range map[string]string{
		"mainagent": "mainagent",
		"planner":   "planner",
		"worker":    "worker",
	} {
		resolved, err := s.resolveTaskRoleLLMForLanguage(t, agentKey, responseLanguage(w))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		roles[responseKey] = resolved
	}
	writeJSON(w, http.StatusOK, roles)
}

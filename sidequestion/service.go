package sidequestion

import (
	"context"
	"errors"
	"github.com/sebastian93921/artifex/locale"
	"strings"

	"github.com/Autumn-27/norma/llm"
)

// SideQuestionService has no harness, tool executor, transcript writer or model
// failover chain. Answer is one completion; Respond adds bounded preparation
// and at most one context-overflow recovery around that completion.
type SideQuestionService struct{ Provider llm.Provider }

type Answer struct {
	Text    string
	Usage   llm.Usage
	ToolUse bool
}

func (s SideQuestionService) Answer(ctx context.Context, req llm.CompletionRequest, streaming bool, update func(Answer)) (out Answer, err error) {
	req.System = append(append([]string(nil), req.System...), locale.Text(locale.FromContext(ctx), "Answer this side question in English. Preserve raw evidence, user content, code, tool arguments, and identifiers exactly."))
	if streaming {
		complete := false
		for ev, streamErr := range s.Provider.Stream(ctx, req) {
			if streamErr != nil {
				err = streamErr
				break
			}
			switch ev.Type {
			case llm.SETextDelta:
				out.Text += ev.Text
			case llm.SEToolUseStart:
				out.ToolUse = true
			case llm.SEMessageStart, llm.SEMessageDelta:
				out.Usage.Add(ev.Usage)
			case llm.SEMessageStop:
				complete = true
			}
			if update != nil {
				update(out)
			}
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
		}
		if err == nil && !complete {
			err = errors.New(locale.Text(locale.FromContext(ctx), "Model response was interrupted; please ask again"))
		}
	} else {
		var msg llm.Message
		msg, _, out.Usage, err = s.Provider.Complete(ctx, req)
		out.Text, out.ToolUse = msg.Text(), len(msg.ToolUses()) > 0
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && strings.TrimSpace(out.Text) == "" {
		if out.ToolUse {
			out.Text = locale.Text(locale.FromContext(ctx), "Side questions cannot execute tools; submit operational requests in the main conversation.")
		} else {
			err = errors.New(locale.Text(locale.FromContext(ctx), "The model returned no answer"))
		}
	}
	return out, err
}

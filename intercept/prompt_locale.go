package intercept

import "github.com/sebastian93921/artifex/locale"

func init() {
	locale.Register(JudgeContextBoundary, JudgeContextBoundary)
	locale.Register(JudgeOutputContract, JudgeOutputContract)
	locale.Register(DefaultJudgePrompt, DefaultJudgePrompt)
}

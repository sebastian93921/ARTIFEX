package intercept

import "github.com/Autumn-27/artex/locale"

func init() {
	locale.Register(JudgeContextBoundary, JudgeContextBoundary)
	locale.Register(JudgeOutputContract, JudgeOutputContract)
	locale.Register(DefaultJudgePrompt, DefaultJudgePrompt)
}

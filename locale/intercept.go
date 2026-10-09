package locale

func init() {
	Register("Review context is incomplete; human confirmation is required: ", "Review context is incomplete; human confirmation is required: ")
	Register("Model review failed; applying failure policy: ", "Model review failed; applying failure policy: ")
	Register("Model output could not be parsed; applying failure policy", "Model output could not be parsed; applying failure policy")
	Register("[Model] ", "[Model] ")
	Register("[Model]", "[Model]")
	Register("Allow", "Allow")
	Register("Block", "Block")
	Register("Request human approval", "Request human approval")
	Register("Interception rule [", "Interception rule [")
	Register("] prohibits this tool call", "] prohibits this tool call")
	Register("] requires user approval; please wait", "] requires user approval; please wait")
	Register("Tool %s requests approval (#%d)", "Tool %s requests approval (#%d)")
	Register("Work was cancelled", "Work was cancelled")
	Register("Work was cancelled before execution", "Work was cancelled before execution")
	Register("Approval timed out; applying timeout policy", "Approval timed out; applying timeout policy")
	Register("Approval was already resolved or does not exist; refresh the record", "Approval was already resolved or does not exist; refresh the record")
	Register("Human denied execution", "Human denied execution")
	Register("Human allowed execution", "Human allowed execution")
	Register("Tool arguments are not valid JSON", "Tool arguments are not valid JSON")
	Register("Execution ended without a tool result", "Execution ended without a tool result")
}

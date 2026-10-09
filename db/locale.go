package db

import "github.com/sebastian93921/artifex/locale"

// legacyBuiltinMetadata contains exact upstream seed values solely for compatibility.
// Stored user text is never rewritten; only known defaults are rendered differently.
var legacyBuiltinMetadata = map[string]string{
	"Goal decomposition": "\u76ee\u6807\u62c6\u89e3",
	"Break a penetration-testing objective into independent, verifiable subgoals.": "\u628a\u6e17\u900f\u4efb\u52a1\u76ee\u6807\u62c6\u89e3\u6210\u82e5\u5e72\u72ec\u7acb\u3001\u53ef\u9a8c\u8bc1\u7684\u5b50\u76ee\u6807\u3002",
	"Task description (target/background)":                                         "\u4efb\u52a1\u63cf\u8ff0\uff08\u6d4b\u8bd5\u5bf9\u8c61/\u80cc\u666f\uff09",
	"Test the example.com website":                                                 "\u6d4b\u8bd5 example.com \u7ad9\u70b9",
	"Planner":                                                                      "\u89c4\u5212",
	"Review the situation and goals, adding exploration intents only for genuinely uncovered directions (one planning loop per task).": "\u8bfb\u53d6\u6001\u52bf\u3001\u5224\u5b9a\u76ee\u6807\uff0c\u53ea\u5728\u786e\u6709\u672a\u8986\u76d6\u7684\u65b0\u65b9\u5411\u65f6\u8865\u5145\u63a2\u7d22\u610f\u56fe\uff08\u6bcf\u4efb\u52a1\u4e00\u4e2a\u89c4\u5212\u5faa\u73af\uff09\u3002",
	"Overall task objective":                           "\u4efb\u52a1\u603b\u76ee\u6807",
	"Obtain administrator access to example.com":       "\u62ff\u4e0b example.com \u7684\u7ba1\u7406\u5458\u6743\u9650",
	"Asset count/type distribution summary (optional)": "\u8d44\u4ea7\u8ba1\u6570/\u7c7b\u578b\u5206\u5e03\u6458\u8981(\u53ef\u9009)",
	"Main agent": "\u4e3b",
	"Human interface: observe progress and turn user intent into hints or high-priority intents.": "\u4eba\u673a\u63a5\u53e3\uff1a\u89c2\u5bdf\u8fdb\u5c55\uff0c\u628a\u4eba\u7684\u610f\u56fe\u843d\u6210 hint \u6216\u9ad8\u4f18\u5148\u7ea7\u610f\u56fe\u3002",
	"Current task objective":                     "\u5f53\u524d\u4efb\u52a1\u76ee\u6807",
	"Initial situation summary (optional)":       "\u5f00\u5c40\u6001\u52bf\u6458\u8981(\u53ef\u9009)",
	"Confirmed vulnerability summary (optional)": "\u5df2\u786e\u8ba4\u6f0f\u6d1e\u6458\u8981(\u53ef\u9009)",
	"Worker": "\u6267\u884c",
	"Claim and execute one intent, write facts and vulnerabilities back to the knowledge graph, then stop.": "\u9886\u53d6\u4e00\u6761\u610f\u56fe\u6267\u884c\uff0c\u628a\u53d1\u73b0\u7684\u4e8b\u5b9e/\u6f0f\u6d1e\u5199\u56de\u77e5\u8bc6\u56fe\u8c31\u540e\u505c\u6b62\u3002",
	"Recording proxy address (controls conditional prompt text)":                                            "\u8bb0\u5f55\u4ee3\u7406\u5730\u5740(\u9a71\u52a8 if \u53cc\u6587\u6848)",
	"Worker identity (optional)": "worker \u81ea\u6211\u6807\u8bc6(\u53ef\u9009)",
	"Platform assistant: manage tasks (create, inspect, pause, hint) and assets, and create or edit skills, custom tools, and MCP servers.": "\u5e73\u53f0\u64cd\u4f5c\u52a9\u624b\uff1a\u7528\u5de5\u5177\u7ba1\u7406\u4efb\u52a1(\u5efa/\u770b/\u6682\u505c/\u7ed9\u63d0\u793a)\u4e0e\u8d44\u4ea7\uff0c\u5e76\u53ef\u521b\u5efa/\u4fee\u6539 skill\u3001\u81ea\u5b9a\u4e49\u5de5\u5177\u3001MCP\u3002",
	"Penetration testing": "\u6e17\u900f\u6d4b\u8bd5",
	"Independent penetration-testing agent: handles reconnaissance, attack-surface discovery, exploitation, verification, and wrap-up with its own planning, execution, and adversarial validation.": "\u72ec\u7acb\u6e17\u900f agent\uff1a\u4e00\u4eba\u4ece\u4fa6\u5bdf\u2192\u627e\u653b\u51fb\u9762\u2192\u6df1\u5165\u5229\u7528\u2192\u9a8c\u8bc1\u2192\u6536\u5c3e\u8d70\u5b8c\u6574\u6761\u94fe\uff0c\u81ea\u5df1\u89c4\u5212\u3001\u81ea\u5df1\u6267\u884c\u3001\u81ea\u5df1\u5bf9\u6297\u5f0f\u9a8c\u8bc1\u3002",
}

func localizeDefaultMetadata(value, canonical string, lang locale.Lang) string {
	if value == canonical || (legacyBuiltinMetadata[canonical] != "" && value == legacyBuiltinMetadata[canonical]) {
		return locale.Text(lang, canonical)
	}
	return value
}

// LocalizeBuiltinAgentMetadata renders known defaults on an API copy. Custom text is preserved.
func LocalizeBuiltinAgentMetadata(a *Agent, lang locale.Lang) {
	if a == nil || !a.Builtin {
		return
	}
	for _, builtin := range builtinAgents {
		if a.Key == builtin.key {
			a.Name = localizeDefaultMetadata(a.Name, builtin.name, lang)
			a.Description = localizeDefaultMetadata(a.Description, builtin.desc, lang)
			return
		}
	}
}

// LocalizeBuiltinPromptVars renders only this agent's matching variable defaults on an API copy.
func LocalizeBuiltinPromptVars(agentKey string, vars []PromptVar, lang locale.Lang) {
	for _, builtin := range builtinAgents {
		if builtin.key != agentKey {
			continue
		}
		for i := range vars {
			for _, v := range builtin.vars {
				if vars[i].Name != v.name {
					continue
				}
				vars[i].Description = localizeDefaultMetadata(vars[i].Description, v.desc, lang)
				vars[i].Example = localizeDefaultMetadata(vars[i].Example, v.example, lang)
			}
		}
	}
}

// A default without a legacy translation must not treat a user's empty value as stock.
func legacyDefaultMetadata(canonical string) string {
	if old, ok := legacyBuiltinMetadata[canonical]; ok {
		return old
	}
	return canonical
}

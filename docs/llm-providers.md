# LLM provider templates

ARTIFEX uses its existing provider formats and custom base URL fields. A template only fills a **new profile's form**; it does not contact a provider, save credentials, activate a profile, or modify existing profiles.

## Z.ai GLM-5.3

No template preview is included; the template only fills the new-profile form.

Open **LLM → New → Configuration template**. Choose **General API (recommended)** for API billing, or **Coding Plan (reference)** only after obtaining Z.ai authorization for ARTIFEX.

| Field | General API | Coding Plan reference |
| --- | --- | --- |
| Format | OpenAI Chat Completions | OpenAI Chat Completions |
| Base URL | `https://api.z.ai/api/paas/v4` | `https://api.z.ai/api/coding/paas/v4` |
| Model | `glm-5.3` | `glm-5.3` |
| Thinking | `enabled` | `enabled` |
| Reasoning effort | `max` | `max` |
| Context window (K) | `1000` | `1000` |
| Output limit | `0` (omit; provider default) | `0` (omit; provider default) |

The endpoints and billing are distinct: General API usage is separate from Coding Plan subscription quota. See [Z.ai connection configuration](https://zcode.z.ai/en/docs/configuration). Confirm your account's model access and billing before testing.

GLM-5.3 accepts text, has a 1M-token context window, and requires reasoning to remain enabled. Supported effort values are `low`, `high`, and `max`; this template uses `max`. The model field remains editable, so review reasoning and context settings when choosing another model. See the [official GLM-5.3 guide](https://docs.z.ai/guides/llm/glm-5.3).

**Coding Plan support boundary:** Z.ai limits Coding Plan to officially supported tools. ARTIFEX is not on that list; an endpoint template does not establish permission or support. Obtain Z.ai authorization before using it here. See [tool integration](https://docs.z.ai/devpack/tool/others) and [usage policy](https://docs.z.ai/devpack/usage-policy). ARTIFEX does not impersonate supported tools or switch billing endpoints automatically.

Enter a profile name and your own API key. Applying either template preserves any name, key, proxy, session-header setting, and other unrelated preferences already entered. Save when ready; a new profile must be activated separately. The existing **Test connection** and **Load models** controls make real provider requests only when clicked; this change does not perform a live provider test.

Verified against official documentation on **2026-10-03**. Provider availability, account entitlement, and policies may change.

## Local models (Ollama, vLLM, LM Studio, llama.cpp)

Any OpenAI-compatible endpoint works as a profile: **LLM → New**, Format `OpenAI Chat Completions`, your `Base URL` and `Model`. The **API key is optional for custom base URLs** — local runtimes typically need none; leave the field blank (Ollama, LM Studio, vLLM without `--api-key`). Set one only if your endpoint enforces it (vLLM `--api-key`, LiteLLM proxy, etc.).

| Runtime | Base URL | Example model |
| --- | --- | --- |
| Ollama | `http://127.0.0.1:11434/v1` | `qwen3:8b` |
| LM Studio | `http://127.0.0.1:1234/v1` | loaded model id |
| vLLM | `http://127.0.0.1:8000/v1` | served model name |
| llama.cpp server | `http://127.0.0.1:8080/v1` | `default` |

Anthropic-format local proxies work the same way with Format `Anthropic`.

Environment bootstrap (when no profile is saved yet) follows the same rule — a custom base URL makes the key optional:

```sh
ARTIFEX_LLM_PROVIDER=openai
ARTIFEX_LLM_BASE_URL=http://127.0.0.1:11434/v1
ARTIFEX_LLM_MODEL=qwen3:8b
# OPENAI_API_KEY unset — accepted because a base URL is configured
```

Use **Test connection** to verify the endpoint before relying on it. Agentic penetration testing needs long-context, tool-calling-capable models; small local models will plan poorly and loop.


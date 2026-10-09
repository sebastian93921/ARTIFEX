"use client";

import { translate as swt } from "@/i18n/runtime";
import { useI18n } from "@/i18n";
import * as React from "react";

import {
  Loader2Icon,
  PlugZapIcon,
  PlusIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SaveIcon,
  StarIcon,
  Trash2Icon,
  ZapIcon,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { applyLLMProfilePreset, type LLMProfilePresetId } from "@/lib/llm-profile-presets";
import { api } from "@/lib/api";
import type { LLMPoolMember, LLMPoolStatus, LLMProfile, LLMRetryOverride } from "@/lib/types";
import { cn } from "@/lib/utils";

import { ProfileRetryFields, RetryPolicyPanel, ZERO_OVERRIDE } from "./_components/retry";

// thinking.type and reasoning_effort are independent fields and must be set separately;
// some APIs have no thinking field and enable reasoning through effort alone.
// Stored empty strings omit the field. Radix Select rejects empty values, so UI uses none
// as a sentinel converted to/from empty strings by NONE/fromStore/toStore.
const NONE = "none";
const fromStore = (v?: string) => (v ? v : NONE);
const toStore = (v: string) => (v === NONE ? "" : v);
const THINKING_TYPES: { value: string; label: string }[] = [
  { value: NONE, get label() { return swt("interface.m1329"); } },
  { value: "disabled", get label() { return swt("interface.m0211"); } },
  { value: "enabled", get label() { return swt("interface.m0210"); } },
];
// Output-limit request key matters only for OpenAI format; reuse NONE/empty-string conversion.
const MAX_TOKENS_FIELDS: { value: string; label: string }[] = [
  { value: NONE, get label() { return swt("interface.m1330"); } },
  { value: "max_completion_tokens", label: "max_completion_tokens" },
];
// The other formats have fixed field names; explain that this selection does not affect them.
const MAX_TOKENS_FIELD_HINTS: Record<string, string> = {
  get openai() { return swt("interface.m1331"); },
  get anthropic() { return swt("interface.m1332"); },
  get "openai-responses"() { return swt("interface.m1333"); },
};
const EFFORT_LEVELS: { value: string; label: string }[] = [
  { value: NONE, get label() { return swt("interface.m1329"); } },
  { value: "low", get label() { return swt("english.e089"); } },
  { value: "medium", get label() { return swt("english.e090"); } },
  { value: "high", get label() { return swt("english.e091"); } },
  { value: "xhigh", get label() { return swt("english.e092"); } },
  { value: "max", get label() { return swt("english.e093"); } },
];

function cooldownText(secs: number) {
  if (secs <= 0) return "";
  if (secs < 60) return `${secs}s`;
  return `${Math.ceil(secs / 60)}min`;
}

// Profile health prioritizes missing API keys because requests cannot be sent at all;
// other states come from rotation circuit records. With rotation off, Healthy means no known failures.
type Health = { label: string; cls: string; hint?: string };
function healthOf(p: LLMProfile, m?: LLMPoolMember): Health {
  if (!p.api_key_hint) {
    return {
      label: swt("interface.m1334"),
      cls: "border-muted-foreground/40 text-muted-foreground",
      hint: swt("interface.m1335"),
    };
  }
  if (m?.state === "tripped") {
    return {
      label: m.cooldown_secs > 0 ? swt("interface.m1336", { p0: cooldownText(m.cooldown_secs) }) : swt("interface.m1337"),
      cls: "border-destructive/50 text-destructive",
      hint: m.last_error,
    };
  }
  if (m?.state === "degraded") {
    return {
      label: swt("interface.m1338", { p0: m.fails }),
      cls: "border-amber-500/50 text-amber-600 dark:text-amber-400",
      hint: m.last_error,
    };
  }
  return { label: swt("interface.m1339"), cls: "border-emerald-500/50 text-emerald-600 dark:text-emerald-400" };
}

// ─────────────────────────────────────────────────────────────────────────────
// Rotation configuration drawer.
// ─────────────────────────────────────────────────────────────────────────────

function PoolSheet({
  open,
  onOpenChange,
  pool,
  onReload,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  pool: LLMPoolStatus | null;
  onReload: () => Promise<void>;
}) {
  "use no memo";
  const { t: swt, locale: swLocale } = useI18n();

  const [busy, setBusy] = React.useState(false);

  // Backend computes cooldown seconds. Poll only with the drawer open and unhealthy profiles present.
  React.useEffect(() => {
    if (!open || !pool?.enabled || !pool.chain.some((m) => m.state !== "ok")) return;
    const t = setInterval(() => void onReload(), 10_000);
    return () => clearInterval(t);
  }, [open, pool, onReload]);

  async function toggle(patch: { llm_pool_enabled?: boolean; llm_pool_bind_fallback?: boolean }) {
    if (busy) return;
    setBusy(true);
    try {
      await api.setSettings(patch);
      await onReload();
      if (patch.llm_pool_enabled !== undefined) {
        toast.success(patch.llm_pool_enabled ? swt("interface.m1340") : swt("interface.m1341"));
      } else {
        toast.success(swt("interface.m1342"));
      }
    } catch (e) {
      toast.error(swt("interface.m1343", { p0: (e as Error).message }));
    } finally {
      setBusy(false);
    }
  }

  async function recover(id?: string) {
    try {
      await api.resetLLMPool(id);
      await onReload();
      toast.success(id ? swt("interface.m1344") : swt("interface.m1345"));
    } catch (e) {
      toast.error(swt("interface.m1346", { p0: (e as Error).message }));
    }
  }

  const enabled = pool?.enabled ?? false;
  const chain = pool?.chain ?? [];
  // Rotation participants exclude opted-out profiles and retain the backend's actual attempt order.
  const inChain = chain.filter((m) => m.active || !m.excluded);
  const tripped = chain.filter((m) => m.state === "tripped");

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex flex-col gap-0 p-0 data-[side=right]:sm:max-w-lg">
        <SheetHeader className="px-4">
          <SheetTitle className="flex items-center gap-2">
            <ZapIcon className="size-4" /> {swt("interface.m1347")}</SheetTitle>
          <SheetDescription>
            {swt("interface.m1348")}<b>{swt("interface.m1349")}</b>{swt("interface.m1350")}</SheetDescription>
        </SheetHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-6">
          <div className="flex items-center justify-between gap-4 rounded-lg border p-3">
            <div className="grid gap-0.5">
              <Label className="text-sm">{swt("interface.m1351")}</Label>
              <p className="text-muted-foreground text-xs">{swt("interface.m1352")}</p>
            </div>
            <Switch
              checked={enabled}
              disabled={busy}
              onCheckedChange={(v) => void toggle({ llm_pool_enabled: v })}
              aria-label={swt("interface.m1353")}
            />
          </div>

          {enabled && (
            <>
              <div className="flex items-center justify-between gap-4 rounded-lg border p-3">
                <div className="grid gap-0.5">
                  <Label className="text-sm">{swt("interface.m1354")}</Label>
                  <p className="text-muted-foreground text-xs">
                    {swt("interface.m1355")}</p>
                </div>
                <Switch
                  checked={pool?.bind_fallback ?? false}
                  disabled={busy}
                  onCheckedChange={(v) => void toggle({ llm_pool_bind_fallback: v })}
                  aria-label={swt("interface.m1356")}
                />
              </div>

              <Separator />

              <div className="grid gap-2">
                <div className="flex items-center justify-between">
                  <Label className="text-sm">{swt("interface.m1357")}</Label>
                  {tripped.length > 0 && (
                    <Button size="sm" variant="ghost" onClick={() => void recover()}>
                      <RotateCcwIcon /> {swt("interface.m1358")}</Button>
                  )}
                </div>
                {inChain.length < 2 && (
                  <p className="text-muted-foreground text-xs">
                    {swt("interface.m1359")}{" "}{inChain.length} {swt("interface.m1360")}</p>
                )}
                {chain.map((m) => {
                  const excluded = m.excluded && !m.active;
                  const order = excluded ? null : inChain.findIndex((x) => x.profile_id === m.profile_id) + 1;
                  return (
                    <div
                      key={m.profile_id}
                      className={cn(
                        "grid gap-1 rounded-lg border p-2.5 text-sm",
                        excluded && "opacity-55",
                        m.state === "tripped" && "border-destructive/40",
                      )}
                    >
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <span className="w-5 shrink-0 text-center font-mono text-muted-foreground text-xs">
                          {order ?? "—"}
                        </span>
                        <span className="font-medium">{m.name}</span>
                        {m.active && (
                          <Badge variant="outline" className="border-amber-400/50 text-amber-500">
                            {swt("interface.m1361")}</Badge>
                        )}
                        {excluded && <Badge variant="outline">{swt("interface.m1362")}</Badge>}
                        <div className="ml-auto flex items-center gap-2">
                          {m.state === "tripped" && m.cooldown_secs > 0 && (
                            <span className="text-muted-foreground text-xs">{swt("interface.m1363")}{" "}{cooldownText(m.cooldown_secs)}</span>
                          )}
                          {m.state === "degraded" && (
                            <span className="text-muted-foreground text-xs">{swt("interface.m1364")}{" "}{m.fails} {swt("interface.m1365")}</span>
                          )}
                          {m.state !== "ok" && (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="size-7"
                              aria-label={swt("interface.m1366")}
                              title={swt("interface.m1367")}
                              onClick={() => void recover(m.profile_id)}
                            >
                              <RotateCcwIcon className="size-3.5" />
                            </Button>
                          )}
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center gap-x-3 pl-7 text-muted-foreground text-xs">
                        <code className="truncate font-mono">{m.model}</code>
                        {!m.active && <span>{swt("interface.m1243")}{" "}{m.priority}</span>}
                      </div>
                      {m.last_error && (
                        <p className="truncate pl-7 font-mono text-muted-foreground text-xs" title={m.last_error}>
                          {m.last_error}
                        </p>
                      )}
                    </div>
                  );
                })}
                {chain.length === 0 && (
                  <div className="rounded-lg border border-dashed p-4 text-center text-muted-foreground text-sm">
                    {swt("interface.m1368")}</div>
                )}
              </div>

              <div className="rounded-lg border border-dashed p-3 text-muted-foreground text-xs leading-relaxed">
                {swt("interface.m1369")}</div>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared model-profile create/edit drawer.
// ─────────────────────────────────────────────────────────────────────────────

function ProfileSheet({
  profile,
  open,
  onOpenChange,
  onSaved,
}: {
  profile: LLMProfile | null; // Null means creation.
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onSaved: (id: string) => void;
}) {
  "use no memo";
  const { t: swt, locale: swLocale } = useI18n();

  const isNew = !profile;
  const [preset, setPreset] = React.useState<LLMProfilePresetId | "">("");
  const [name, setName] = React.useState("");
  const [format, setFormat] = React.useState<"anthropic" | "openai" | "openai-responses">("anthropic");
  const [model, setModel] = React.useState("");
  const [baseUrl, setBaseUrl] = React.useState("");
  const [proxy, setProxy] = React.useState("");
  const [apiKey, setApiKey] = React.useState("");
  const [keyHint, setKeyHint] = React.useState("");
  const [rps, setRps] = React.useState("0");
  const [rpm, setRpm] = React.useState("0");
  const [cw, setCw] = React.useState("0"); // Context window in K tokens; 0 defaults to 200K.
  const [thinkingType, setThinkingType] = React.useState(NONE);
  const [effort, setEffort] = React.useState(NONE);
  const [priority, setPriority] = React.useState("0"); // Rotation priority; higher values first.
  const [poolExclude, setPoolExclude] = React.useState(false);
  const [streaming, setStreaming] = React.useState(true); // True means streaming (default), false means nonstreaming.
  const [maxTokens, setMaxTokens] = React.useState("0"); // Per-response output limit; 0 omits the field.
  const [maxTokensField, setMaxTokensField] = React.useState(NONE); // Output-limit field name; NONE means max_tokens.
  const [sessionHeaderKey, setSessionHeaderKey] = React.useState(""); // Custom session header name; blank omits it.
  const [maxConcurrent, setMaxConcurrent] = React.useState("0"); // In-flight request cap; 0 = unlimited.
  const [retry, setRetry] = React.useState<LLMRetryOverride>(ZERO_OVERRIDE); // Profile retry overrides; all zeros follow global settings.
  const [testing, setTesting] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const [models, setModels] = React.useState<string[]>([]);
  const [loadingModels, setLoadingModels] = React.useState(false);
  const [modelsOpen, setModelsOpen] = React.useState(false);

  // Populate from the supplied profile on every open, or reset defaults for creation.
  // Closing and reopening starts fresh without stale values from another profile.
  React.useEffect(() => {
    if (!open) return;
    setPreset("");
    setName(profile?.name ?? "");
    setFormat(profile?.format === "openai" || profile?.format === "openai-responses" ? profile.format : "anthropic");
    setModel(profile?.model ?? "");
    setBaseUrl(profile?.base_url ?? "");
    setProxy(profile?.proxy ?? "");
    setRps(String(profile?.rate_per_second ?? 0));
    setRpm(String(profile?.rate_per_minute ?? 0));
    setCw(String(profile?.context_window_k ?? 0));
    setThinkingType(fromStore(profile?.thinking_type));
    setEffort(fromStore(profile?.reasoning_effort));
    setPriority(String(profile?.priority ?? 0));
    setPoolExclude(profile?.pool_exclude ?? false);
    setStreaming(profile?.streaming ?? true);
    setMaxTokens(String(profile?.max_tokens ?? 0));
    setMaxTokensField(fromStore(profile?.max_tokens_field));
    setSessionHeaderKey(profile?.session_header_key ?? "");
    setMaxConcurrent(String(profile?.max_concurrent ?? 0));
    setRetry(profile?.retry ?? ZERO_OVERRIDE);
    setApiKey("");
    setKeyHint(profile?.api_key_hint ?? "");
    setModels([]);
    setModelsOpen(false);
  }, [open, profile]);

  function applyPreset(id: string) {
    if (!isNew) return;
    const next = applyLLMProfilePreset({ name, api_key: apiKey, proxy, session_header_key: sessionHeaderKey }, id);
    setPreset(id as LLMProfilePresetId);
    setFormat(next.format);
    setModel(next.model);
    setBaseUrl(next.base_url);
    setThinkingType(next.thinking_type);
    setEffort(next.reasoning_effort);
    setCw(String(next.context_window_k));
    setMaxTokens(String(next.max_tokens));
    setMaxTokensField(fromStore(next.max_tokens_field));
    setModels([]);
    setModelsOpen(false);
  }

  const profileId = profile ? Number(profile.id) : undefined;

  async function loadModels() {
    if (loadingModels) return;
    setLoadingModels(true);
    setModels([]);
    try {
      const r = await api.fetchLLMModels(format, baseUrl, apiKey, proxy, profileId);
      if (r.ok && r.models && r.models.length > 0) {
        setModels(r.models);
        setModelsOpen(true);
        toast.success(swt("interface.m1370", { p0: r.models.length }));
      } else {
        toast.error(swt("interface.m1371", { p0: r.error ?? swt("interface.m1372") }));
      }
    } catch (e) {
      toast.error(swt("interface.m1373", { p0: (e as Error).message }));
    } finally {
      setLoadingModels(false);
    }
  }

  async function testConnection() {
    if (testing) return;
    setTesting(true);
    try {
      // Test using the profile's actual reasoning parameters so unsupported fields fail here,
      // not during tasks. Pass profile ID to use the stored key when the key input is blank.
      const r = await api.testLLM(
        format,
        model,
        baseUrl,
        apiKey,
        proxy,
        toStore(thinkingType),
        toStore(effort),
        profileId,
        streaming,
        sessionHeaderKey.trim(),
      );
      // Show response content to prove the model actually answered as it would in a conversation.
      if (r.ok)
        toast.success(swt("interface.m1374", { p0: r.latency_ms ?? "?", p1: r.model ?? model }), {
          description: r.reply ? swt("interface.m1375", { p0: r.reply }) : undefined,
        });
      else toast.error(swt("interface.m1376", { p0: r.error ?? swt("interface.m1377") }));
    } catch (e) {
      toast.error(swt("interface.m1378", { p0: (e as Error).message }));
    } finally {
      setTesting(false);
    }
  }

  async function save() {
    if (!name.trim() || !model.trim()) {
      toast.error(swt("interface.m1379"));
      return;
    }
    if (saving) return;
    setSaving(true);
    try {
      const { id } = await api.saveLLMProfile({
        ...(profile ? { id: Number(profile.id) } : {}),
        name: name.trim(),
        format,
        model: model.trim(),
        base_url: baseUrl.trim(),
        proxy: proxy.trim(),
        api_key: apiKey,
        rate_per_second: Number(rps) || 0,
        rate_per_minute: Number(rpm) || 0,
        context_window_k: Number(cw) || 0,
        thinking_type: toStore(thinkingType),
        reasoning_effort: toStore(effort),
        priority: Number(priority) || 0,
        pool_exclude: poolExclude,
        streaming,
        max_tokens: Math.max(0, Number(maxTokens) || 0),
        // Output-field selection affects only OpenAI Chat Completions; other formats use defaults.
        // The backend normalizes again; this also prevents contradictory UI submissions.
        max_tokens_field: format === "openai" ? toStore(maxTokensField) : "",
        session_header_key: sessionHeaderKey.trim(),
        max_concurrent: Math.max(0, Number(maxConcurrent) || 0),
        retry,
      });
      if (isNew) toast.success(swt("interface.m1380", { p0: name.trim() }));
      else toast.success(profile?.is_default ? swt("interface.m1381") : swt("interface.m0377"));
      onSaved(String(id));
      onOpenChange(false);
    } catch (e) {
      toast.error(swt("interface.m0267", { p0: (e as Error).message }));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex flex-col gap-0 p-0 data-[side=right]:min-w-[420px] data-[side=right]:sm:max-w-xl"
      >
        <SheetHeader className="px-4">
          <SheetTitle className="flex items-center gap-2">
            {isNew ? swt("interface.m1382") : swt("interface.m1383", { p0: profile?.name })}
            {profile?.is_default && (
              <Badge variant="outline" className="border-amber-400/50 text-amber-500">
                {swt("interface.m1384")}</Badge>
            )}
          </SheetTitle>
          <SheetDescription>
            {isNew
              ? swt("interface.m1385")
              : swt("interface.m1386")}
          </SheetDescription>
        </SheetHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4">
          {isNew && (
            <div className="grid gap-2 rounded-md border bg-muted/20 p-3">
              <Label htmlFor="p-preset">{swt("providers.template")}</Label>
              <Select value={preset} onValueChange={applyPreset}>
                <SelectTrigger id="p-preset" className="w-full min-w-0">
                  <SelectValue placeholder={swt("providers.chooseTemplate")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="zai-general">{swt("providers.general")}</SelectItem>
                  <SelectItem value="zai-coding-reference">{swt("providers.coding")}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{swt("providers.introduction")}</p>
              {preset && (
                <>
                  <p className={preset === "zai-coding-reference" ? "text-xs text-amber-700 dark:text-amber-400" : "text-xs text-muted-foreground"}>
                    {swt(preset === "zai-coding-reference" ? "providers.codingHelp" : "providers.generalHelp")}
                  </p>
                  <p className="text-xs text-muted-foreground">{swt("providers.modelHelp")}</p>
                  <div className="flex flex-wrap gap-3 text-xs">
                    <a className="underline underline-offset-4" href="https://docs.z.ai/guides/llm/glm-5.3" target="_blank" rel="noreferrer">{swt("providers.officialModel")}</a>
                    <a className="underline underline-offset-4" href="https://docs.z.ai/devpack/usage-policy" target="_blank" rel="noreferrer">{swt("providers.officialPolicy")}</a>
                  </div>
                </>
              )}
            </div>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="p-name">{swt("interface.m0868")}</Label>
              <Input
                id="p-name"
                placeholder={swt("interface.m1387")}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label>{swt("interface.m1388")}</Label>
              <Select value={format} onValueChange={(v) => setFormat(v as "anthropic" | "openai" | "openai-responses")}>
                <SelectTrigger>
                  <SelectValue placeholder={swt("interface.m1389")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="anthropic">Anthropic</SelectItem>
                  <SelectItem value="openai">OpenAI (Chat Completions)</SelectItem>
                  <SelectItem value="openai-responses">OpenAI (Responses API)</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="p-model">{swt("interface.m0627")}</Label>
            <div className="flex gap-2">
              <Input
                id="p-model"
                className="font-mono"
                placeholder="claude-opus-4-8"
                value={model}
                onChange={(e) => setModel(e.target.value)}
              />
              {/* This body-portaled Popover sits outside Sheet's scroll lock. modal gives it the top scroll lock so its list can scroll. */}
              <Popover open={modelsOpen} onOpenChange={setModelsOpen} modal>
                <PopoverTrigger asChild>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    className="shrink-0"
                    disabled={loadingModels}
                    onClick={loadModels}
                    title={swt("interface.m1390")}
                  >
                    {loadingModels ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />}
                  </Button>
                </PopoverTrigger>
                {models.length > 0 && (
                  <PopoverContent className="max-h-72 w-72 gap-0 overflow-y-auto overscroll-contain p-1" align="end">
                    {models.map((m) => (
                      <button
                        key={m}
                        type="button"
                        className="w-full shrink-0 rounded-md px-2 py-1.5 text-left font-mono text-xs hover:bg-accent hover:text-accent-foreground"
                        onClick={() => {
                          setModel(m);
                          setModelsOpen(false);
                        }}
                      >
                        {m}
                      </button>
                    ))}
                  </PopoverContent>
                )}
              </Popover>
            </div>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="p-base-url">{swt("interface.m1391")}</Label>
            <Input
              id="p-base-url"
              className="font-mono"
              placeholder="https://api.openai.com/v1"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
            />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="p-proxy">{swt("interface.m1392")}</Label>
            <Input
              id="p-proxy"
              className="font-mono"
              placeholder="socks5://user:pass@127.0.0.1:1080 · http://127.0.0.1:8080"
              value={proxy}
              onChange={(e) => setProxy(e.target.value)}
            />
            <p className="text-muted-foreground text-xs">
              {swt("interface.m1393")}</p>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="p-session-header">{swt("interface.m1394")}</Label>
            <Input
              id="p-session-header"
              className="font-mono"
              placeholder={swt("interface.m1395")}
              value={sessionHeaderKey}
              onChange={(e) => setSessionHeaderKey(e.target.value)}
            />
            <p className="text-muted-foreground text-xs">
              {swt("interface.m1396")}<b>{swt("interface.m1397")}</b>{swt("interface.m1398")}</p>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="p-api-key">{swt("english.e100")}</Label>
            <Input
              id="p-api-key"
              type="password"
              placeholder={keyHint ? swt("interface.m1399", { p0: keyHint }) : "sk-…"}
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
            />
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <div className="grid gap-2">
              <Label htmlFor="p-rps">{swt("interface.m1400")}</Label>
              <Input id="p-rps" type="number" min={0} value={rps} onChange={(e) => setRps(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="p-rpm">{swt("interface.m1401")}</Label>
              <Input id="p-rpm" type="number" min={0} value={rpm} onChange={(e) => setRpm(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="p-cw">{swt("interface.m1402")}</Label>
              <Input
                id="p-cw"
                type="number"
                min={0}
                max={1000}
                value={cw}
                onChange={(e) => setCw(e.target.value)}
                placeholder="200"
              />
            </div>
          </div>
          <p className="-mt-2 text-muted-foreground text-xs">
            {swt("interface.m1403")}</p>

          <div className="grid gap-3 rounded-lg border p-3">
            <div className="flex items-center justify-between gap-4">
              <div className="grid gap-0.5">
                <Label htmlFor="p-priority" className="text-sm">
                  {swt("interface.m1404")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m1405")}</p>
              </div>
              <Input
                id="p-priority"
                type="number"
                className="w-24 shrink-0"
                value={priority}
                onChange={(e) => setPriority(e.target.value)}
              />
            </div>
            <div className="flex items-center justify-between gap-4 border-t pt-3">
              <div className="grid gap-0.5">
                <Label className="text-sm">{swt("interface.m1362")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m1406")}</p>
              </div>
              <Switch checked={poolExclude} onCheckedChange={setPoolExclude} aria-label={swt("interface.m1362")} />
            </div>
            <div className="flex items-center justify-between gap-4 border-t pt-3">
              <div className="grid gap-0.5">
                <Label className="text-sm">{swt("interface.m1407")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m1408")}</p>
              </div>
              <Switch checked={streaming} onCheckedChange={setStreaming} aria-label={swt("interface.m1409")} />
            </div>
          </div>

          <div className="grid gap-3 rounded-lg border p-3">
            <div className="flex items-center justify-between gap-4">
              <div className="grid gap-0.5">
                <Label htmlFor="p-max-tokens" className="text-sm">
                  {swt("interface.m1410")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m1411")}</p>
              </div>
              <Input
                id="p-max-tokens"
                type="number"
                min={0}
                className="w-28 shrink-0"
                value={maxTokens}
                onChange={(e) => setMaxTokens(e.target.value)}
                placeholder="0"
              />
            </div>
            <div className="flex items-center justify-between gap-4 border-t pt-3">
              <div className="grid gap-0.5">
                <Label className="text-sm">{swt("interface.m1412")}</Label>
                <p className="text-muted-foreground text-xs">{MAX_TOKENS_FIELD_HINTS[format]}</p>
              </div>
              <Select
                value={format === "openai" ? maxTokensField : NONE}
                onValueChange={setMaxTokensField}
                disabled={format !== "openai"}
              >
                <SelectTrigger className="w-56 shrink-0">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {MAX_TOKENS_FIELDS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid gap-3 rounded-lg border p-3">
            <div className="flex items-center justify-between gap-4">
              <div className="grid gap-0.5">
                <Label htmlFor="p-max-concurrent" className="text-sm">
                  {swt("interface.m2874")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m2875")}</p>
              </div>
              <Input
                id="p-max-concurrent"
                type="number"
                min={0}
                className="w-28 shrink-0"
                value={maxConcurrent}
                onChange={(e) => setMaxConcurrent(e.target.value)}
                placeholder="0"
              />
            </div>
          </div>

          <div className="grid gap-3 rounded-lg border p-3">
            <div className="flex items-center justify-between gap-4">
              <div className="grid gap-0.5">
                <Label className="text-sm">{swt("interface.m1413")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m1414")}</p>
              </div>
              <Select value={thinkingType} onValueChange={setThinkingType}>
                <SelectTrigger className="w-32 shrink-0">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {THINKING_TYPES.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center justify-between gap-4 border-t pt-3">
              <div className="grid gap-0.5">
                <Label className="text-sm">{swt("interface.m1415")}</Label>
                <p className="text-muted-foreground text-xs">
                  {swt("interface.m1416")}</p>
              </div>
              <Select value={effort} onValueChange={setEffort}>
                <SelectTrigger className="w-32 shrink-0">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {EFFORT_LEVELS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <ProfileRetryFields value={retry} onChange={setRetry} />
        </div>

        <div className="flex gap-2 border-t px-4 py-3">
          <Button variant="outline" onClick={testConnection} disabled={testing}>
            {testing ? <Loader2Icon className="animate-spin" /> : <PlugZapIcon />}
            {testing ? swt("interface.m1417") : swt("interface.m1418")}
          </Button>
          <Button onClick={save} disabled={saving} className="flex-1">
            {saving && <Loader2Icon className="animate-spin" />}
            {!saving && (isNew ? <PlusIcon /> : <SaveIcon />)}
            {isNew ? swt("interface.m0728") : swt("interface.m0273")}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ─────────────────────────────────────────────────────────────────────────────

export default function LLMPage() {
  "use no memo";
  const { t: swt, locale: swLocale } = useI18n();

  const [profiles, setProfiles] = React.useState<LLMProfile[]>([]);
  const [pool, setPool] = React.useState<LLMPoolStatus | null>(null);
  const [poolOpen, setPoolOpen] = React.useState(false);
  // Store drawer visibility separately from editing data to avoid the closing animation changing
  // the title from Edit X to New. editing=null means creation.
  const [editOpen, setEditOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<LLMProfile | null>(null);
  const openEditor = React.useCallback((p: LLMProfile | null) => {
    setEditing(p);
    setEditOpen(true);
  }, []);

  const loadPool = React.useCallback(async () => {
    try {
      setPool(await api.llmPool());
    } catch {
      /* ignore */
    }
  }, []);

  const load = React.useCallback(async () => {
    try {
      setProfiles(await api.llmProfiles());
    } catch {
      /* ignore */
    }
    await loadPool();
  }, [loadPool]);

  React.useEffect(() => {
    void load();
  }, [load]);

  // Profile health badges look up rotation state by profile ID.
  const health = React.useMemo(() => {
    const m = new Map<string, LLMPoolMember>();
    for (const c of pool?.chain ?? []) m.set(c.profile_id, c);
    return m;
  }, [swLocale, pool]);

  async function activate(id: string, name: string) {
    try {
      await api.activateLLMProfile(id);
      toast.success(swt("interface.m1419", { p0: name }));
      await load();
    } catch (e) {
      toast.error(swt("interface.m1420", { p0: (e as Error).message }));
    }
  }

  async function remove(p: LLMProfile) {
    if (p.is_default) {
      toast.error(swt("interface.m1421"));
      return;
    }
    try {
      await api.deleteLLMProfile(p.id);
      toast.success(swt("interface.m1422", { p0: p.name }));
      await load();
    } catch (e) {
      toast.error(swt("interface.m0379", { p0: (e as Error).message }));
    }
  }

  const poolOn = pool?.enabled ?? false;

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-semibold text-xl tracking-tight">LLM</h1>
          <p className="text-muted-foreground text-sm">
            {swt("interface.m1423")}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setPoolOpen(true)}>
            <ZapIcon /> {swt("interface.m1424")}{" "}{poolOn && (
              <Badge variant="outline" className="ml-1 border-emerald-500/50 text-emerald-600 dark:text-emerald-400">
                {swt("interface.m1425")}</Badge>
            )}
          </Button>
          <Button size="sm" variant="outline" onClick={() => openEditor(null)}>
            <PlusIcon /> {swt("interface.m0728")}</Button>
        </div>
      </div>

      <Tabs defaultValue="profiles" className="flex-1">
        <TabsList>
          <TabsTrigger value="profiles">{swt("interface.m1240")}</TabsTrigger>
          <TabsTrigger value="retry">{swt("interface.m1426")}</TabsTrigger>
        </TabsList>

        <TabsContent value="profiles" className="mt-4">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {profiles.map((p) => {
              const h = healthOf(p, health.get(p.id));
              return (
                // biome-ignore lint/a11y/useSemanticElements: cards contain action buttons; a native button would create invalid nesting
                <Card
                  key={p.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => openEditor(p)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      openEditor(p);
                    }
                  }}
                  className={cn(
                    "cursor-pointer gap-0 py-4 outline-none transition-colors hover:border-foreground/30",
                    p.is_default && "border-amber-400/50 bg-amber-400/5",
                  )}
                >
                  <CardContent className="grid gap-2 px-4">
                    <div className="flex items-start gap-2">
                      <StarIcon
                        className={cn(
                          "mt-0.5 size-4 shrink-0",
                          p.is_default ? "fill-amber-400 text-amber-400" : "text-muted-foreground",
                        )}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="truncate font-medium text-sm">{p.name}</span>
                          <Badge variant="outline" className="uppercase">
                            {p.format}
                          </Badge>
                          <Badge variant="outline" className={cn("ml-auto", h.cls)} title={h.hint}>
                            {h.label}
                          </Badge>
                        </div>
                        <code className="mt-1 block truncate font-mono text-muted-foreground text-xs">{p.model}</code>
                      </div>
                    </div>

                    <div className="flex flex-wrap gap-x-3 gap-y-0.5 pl-6 text-muted-foreground text-xs">
                      {p.api_key_hint && <span>{p.api_key_hint}</span>}
                      <span>
                        {p.rate_per_second}{swt("english.e102")}{p.rate_per_minute}{swt("english.e103")}</span>
                      {p.proxy && <span className="truncate">{swt("interface.m1427")}{" "}{p.proxy}</span>}
                      {p.reasoning_effort && (
                        <span>{swt("interface.m1428")}{" "}{p.reasoning_effort === "off" ? swt("interface.m1429") : p.reasoning_effort}</span>
                      )}
                      {/* Show rotation fields only while rotation is enabled. */}
                      {poolOn &&
                        !p.is_default &&
                        (p.pool_exclude ? <span>{swt("interface.m1362")}</span> : <span>{swt("interface.m1243")}{" "}{p.priority ?? 0}</span>)}
                    </div>

                    <div className="mt-1 flex gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        disabled={p.is_default}
                        onClick={(e) => {
                          e.stopPropagation();
                          void activate(p.id, p.name);
                        }}
                      >
                        {p.is_default ? swt("interface.m1430") : swt("interface.m1431")}
                      </Button>
                      <Button
                        size="icon"
                        variant="outline"
                        aria-label={swt("interface.m1432")}
                        onClick={(e) => {
                          e.stopPropagation();
                          void remove(p);
                        }}
                      >
                        <Trash2Icon className="text-destructive" />
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
            {profiles.length === 0 && (
              <div className="col-span-full rounded-lg border border-dashed p-10 text-center text-muted-foreground text-sm">
                {swt("interface.m1433")}</div>
            )}
          </div>
        </TabsContent>

        <TabsContent value="retry" className="mt-4">
          <RetryPolicyPanel />
        </TabsContent>
      </Tabs>

      <ProfileSheet profile={editing} open={editOpen} onOpenChange={setEditOpen} onSaved={() => void load()} />
      <PoolSheet open={poolOpen} onOpenChange={setPoolOpen} pool={pool} onReload={loadPool} />
    </div>
  );
}

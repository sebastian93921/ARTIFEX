import { translate as swt } from "@/i18n/runtime";
// Channel field definitions and configuration parsers.
//
// Separate data definitions from views: fields per channel,
// control types, and bidirectional form-text/JSON conversion.
// Adding a channel requires editing this file, not page orchestration.
// Channel display names and descriptions belong to frontend presentation only.
export const KIND_LABEL: Record<string, string> = {
  get dingtalk() { return swt("interface.m1475"); },
  get feishu() { return swt("interface.m1476"); },
  get wecom() { return swt("interface.m1477"); },
  get webhook() { return swt("interface.m1478"); },
  telegram: "Telegram",
  get email() { return swt("interface.m1479"); },
};

// Configuration field definitions per channel.
//
// Keep frontend field definitions rather than backend schemas: the backend handles
// required/format validation, while the UI handles layout and control types.
// The only coupling is secret_keys, supplied by the backend to select password controls,
// because channel implementations know credentials: WeCom's whole webhook is secret,
// while DingTalk has a separate secret. Missing frontend definitions produce a visible
// hasFields warning rather than silent failure.
export type FieldKind = "text" | "password" | "number" | "select" | "textarea" | "switch" | "kv" | "list";
export interface FieldDef {
  key: string;
  label: string;
  kind: FieldKind;
  placeholder?: string;
  help?: string;
  options?: { value: string; label: string }[];
}
export const CHANNEL_FIELDS: Record<string, FieldDef[]> = {
  dingtalk: [
    {
      key: "webhook",
      get label() { return swt("interface.m1480"); },
      kind: "text",
      placeholder: "https://oapi.dingtalk.com/robot/send?access_token=...",
    },
    {
      key: "secret",
      get label() { return swt("interface.m1481"); },
      kind: "password",
      get help() { return swt("interface.m1482"); },
    },
  ],
  feishu: [
    {
      key: "webhook",
      get label() { return swt("interface.m1480"); },
      kind: "text",
      placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/...",
    },
    { key: "secret", get label() { return swt("interface.m1483"); }, kind: "password", get help() { return swt("interface.m1484"); } },
  ],
  wecom: [
    {
      key: "webhook",
      get label() { return swt("interface.m1480"); },
      kind: "text",
      placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...",
    },
  ],
  webhook: [
    { key: "url", get label() { return swt("interface.m1485"); }, kind: "text", placeholder: "https://your-endpoint.example.com/hook" },
    {
      key: "method",
      get label() { return swt("interface.m1486"); },
      kind: "select",
      options: [
        { value: "POST", get label() { return swt("interface.m1487"); } },
        { value: "PUT", get label() { return swt("interface.m1488"); } },
        { value: "PATCH", get label() { return swt("interface.m1489"); } },
        { value: "GET", get label() { return swt("interface.m1490"); } },
      ],
    },
    { key: "headers", get label() { return swt("interface.m1491"); }, kind: "kv", get help() { return swt("interface.m1492"); } },
    {
      key: "body_template",
      get label() { return swt("interface.m1493"); },
      kind: "textarea",
      help:
        swt("interface.m1494") +
        swt("interface.m1495") +
        swt("interface.m1496"),
    },
  ],
  telegram: [
    { key: "bot_token", get label() { return swt("english.e112"); }, kind: "password", placeholder: "123456:ABC-DEF..." },
    { key: "chat_id", get label() { return swt("english.e114"); }, kind: "text", placeholder: "-1001234567890" },
    {
      key: "base_url",
      get label() { return swt("interface.m1497"); },
      kind: "text",
      placeholder: "https://api.telegram.org",
      get help() { return swt("interface.m1498"); },
    },
  ],
  email: [
    { key: "host", get label() { return swt("interface.m1499"); }, kind: "text", placeholder: "smtp.example.com" },
    {
      key: "port",
      get label() { return swt("interface.m0244"); },
      kind: "number",
      placeholder: "587",
      get help() { return swt("interface.m1500"); },
    },
    { key: "username", get label() { return swt("interface.m1501"); }, kind: "text" },
    { key: "password", get label() { return swt("interface.m1502"); }, kind: "password" },
    { key: "from", get label() { return swt("interface.m1503"); }, kind: "text", placeholder: "artifex@example.com" },
    { key: "to", get label() { return swt("interface.m1504"); }, kind: "list", get help() { return swt("interface.m1505"); } },
    { key: "tls", get label() { return swt("interface.m1506"); }, kind: "switch", get help() { return swt("interface.m1507"); } },
  ],
};

export const SEVERITY_OPTIONS = [
  { value: "", get label() { return swt("interface.m1508"); } },
  { value: "low", get label() { return swt("interface.m1509"); } },
  { value: "medium", get label() { return swt("interface.m1510"); } },
  { value: "high", get label() { return swt("interface.m1511"); } },
  { value: "critical", get label() { return swt("interface.m1512"); } },
];

export type ChannelForm = {
  name: string;
  kind: string;
  mode: "realtime" | "digest";
  enabled: boolean;
  ratePerMin: string;
  config: Record<string, unknown>;
  minSeverity: string;
  includeText: string;
  excludeText: string;
  taskIDsText: string;
  assetIDsText: string;
  onStatusChange: boolean;
};

export const emptyForm = (kind: string): ChannelForm => ({
  name: "",
  kind,
  mode: "realtime",
  enabled: true,
  ratePerMin: "",
  config: {},
  minSeverity: "",
  includeText: "",
  excludeText: "",
  taskIDsText: "",
  assetIDsText: "",
  onStatusChange: false,
});

// parseKV parses one KEY=VALUE pair per line.
export function parseKV(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const i = t.indexOf("=");
    if (i > 0) out[t.slice(0, i).trim()] = t.slice(i + 1).trim();
  }
  return out;
}
// parseIDs parses comma/whitespace-separated IDs.
export function parseIDs(text: string): number[] {
  return text
    .split(/[\s,，]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => Number(s))
    .filter((n) => Number.isFinite(n) && n > 0);
}
// parseKeywords splits on lines/commas, preserving spaces inside vulnerability names.
export function parseKeywords(text: string): string[] {
  return text
    .split(/[\n,，]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

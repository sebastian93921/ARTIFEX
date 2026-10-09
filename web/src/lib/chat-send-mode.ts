"use client";

import { translate as swt } from "@/i18n/runtime";
import * as React from "react";

import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

// Conversation send/newline shortcuts are browser-only localStorage preferences, not database/account settings;
// changing browsers requires setup again. Issue #39: 0.3.2 changed Ctrl+Enter to Enter,
// so restore the old shortcut as an option.
export type ChatSendMode = "enter" | "ctrl-enter";

export const CHAT_SEND_MODE_KEY = "artifex_chat_send_mode";
export const DEFAULT_CHAT_SEND_MODE: ChatSendMode = "enter";

export const CHAT_SEND_MODE_OPTIONS: { value: ChatSendMode; label: string }[] = [
  { value: "enter", get label() { return swt("interface.m2421"); } },
  { value: "ctrl-enter", get label() { return swt("interface.m2422"); } },
];

function parseMode(raw: string | null): ChatSendMode {
  return raw === "ctrl-enter" || raw === "enter" ? raw : DEFAULT_CHAT_SEND_MODE;
}

// Same-tab subscribers: localStorage storage events fire only in other tabs,
// so emit local changes to update inputs without reloading.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

// Snapshots are primitive strings, compared by value with Object.is to avoid subscription loops.
function getSnapshot(): ChatSendMode {
  return parseMode(getLocalStorageValue(CHAT_SEND_MODE_KEY));
}

// Server rendering lacks localStorage; render defaults and reconcile after hydration.
function getServerSnapshot(): ChatSendMode {
  return DEFAULT_CHAT_SEND_MODE;
}

export function useChatSendMode(): ChatSendMode {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function setChatSendMode(mode: ChatSendMode) {
  setLocalStorageValue(CHAT_SEND_MODE_KEY, mode);
  for (const listener of listeners) listener();
}

// shouldSubmitOnKey determines whether a keypress sends a message.
// isComposing/keyCode 229 indicate IME composition; Enter must select candidates rather than send.
// Enter mode excludes only Shift, preserving 0.3.2 behavior for unchanged preferences.
// Ctrl+Enter mode also accepts Cmd on macOS.
export function shouldSubmitOnKey(e: React.KeyboardEvent, mode: ChatSendMode): boolean {
  if (e.key !== "Enter") return false;
  if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return false;
  if (mode === "ctrl-enter") return e.ctrlKey || e.metaKey;
  return !e.shiftKey;
}

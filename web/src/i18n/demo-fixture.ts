// Localize authored demo fixtures once, before they become editable runtime data.
// Accessor setters turn edits into ordinary values, so user content is never
// translated or reset when the interface language changes.
import { catalogs, type MessageKey } from "./catalog.ts";
import { translate } from "./runtime.ts";

const authoredKeys = new Map<string, MessageKey>();
for (const [key, value] of Object.entries(catalogs.en)) authoredKeys.set(value, key as MessageKey);

export function cloneDemoFixture<T>(value: T, authored = false): T {
  if (value === null || typeof value !== "object") return value;
  if (value instanceof Date) return new Date(value) as T;
  const copy = (Array.isArray(value) ? [] : {}) as Record<string, unknown>;
  for (const key of Object.keys(value)) {
    const descriptor = Object.getOwnPropertyDescriptor(value, key)!;
    const entry = (value as Record<string, unknown>)[key];
    const message = authored && typeof entry === "string" ? authoredKeys.get(entry) : undefined;
    if (descriptor.get || message) {
      Object.defineProperty(copy, key, {
        enumerable: true,
        configurable: true,
        get: descriptor.get ? () => descriptor.get!.call(copy) : () => translate(message!),
        set(next: unknown) {
          Object.defineProperty(copy, key, { value: next, writable: true, enumerable: true, configurable: true });
        },
      });
    } else copy[key] = cloneDemoFixture(entry, authored);
  }
  return copy as T;
}

export function prepareDemoFixture(value: unknown): void {
  if (value === null || typeof value !== "object") return;
  const prepared = cloneDemoFixture(value, true);
  Object.defineProperties(value, Object.getOwnPropertyDescriptors(prepared));
}

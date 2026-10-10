import { onBeforeUnmount, onMounted, toValue, type MaybeRefOrGetter } from "vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate } from "vue-router";
import type { TypedSchema, TypedSchemaError } from "vee-validate";
import { z } from "zod";
import { en, zhCN } from "zod/locales";
import { currentLocale, translate, type Locale } from "@/shared/i18n";
import { confirm } from "@/shared/lib/confirm";

let zodLocale: Locale | undefined;

/** Switches zod's built-in messages to the UI locale (done lazily so pages without forms never load zod). */
function applyZodLocale(locale: Locale): void {
  if (zodLocale === locale) return;
  zodLocale = locale;
  z.config(locale === "zh-CN" ? zhCN() : en());
}

/**
 * Adapts a zod schema to vee-validate (`useForm({ validationSchema: zodSchema(schema) })`).
 * Generic zod messages follow the UI locale; custom messages should be created
 * with `formError()` so they are translated too.
 */
export function zodSchema<S extends z.ZodType>(schema: S): TypedSchema<z.input<S>, z.output<S>> {
  return {
    __type: "VVTypedSchema",
    async parse(values) {
      applyZodLocale(currentLocale());
      const result = await schema.safeParseAsync(values);
      if (result.success) return { value: result.data, errors: [] };
      const byPath = new Map<string, string[]>();
      for (const issue of result.error.issues) {
        const path = issue.path
          .map((key, index) =>
            typeof key === "number" ? `[${key}]` : index === 0 ? String(key) : `.${String(key)}`,
          )
          .join("");
        byPath.set(path, [...(byPath.get(path) ?? []), issue.message]);
      }
      const errors: TypedSchemaError[] = [...byPath].map(([path, messages]) => ({
        path,
        errors: messages,
      }));
      return { errors };
    },
  };
}

const PREFIX = "i18n:";

/** A custom validation message resolved through vue-i18n when displayed. */
export function formError(key: string, params?: Record<string, unknown>): string {
  return PREFIX + JSON.stringify([key, params ?? {}]);
}

/** Turns a validation message into display text (used by FormField). */
export function displayFormError(message: string | undefined): string | undefined {
  if (!message?.startsWith(PREFIX)) return message;
  try {
    const [key, params] = JSON.parse(message.slice(PREFIX.length)) as [
      string,
      Record<string, unknown>,
    ];
    return translate(key, params);
  } catch {
    return message;
  }
}

/** UTF-8 byte length, for limits the server counts in bytes (passwords). */
export function utf8Length(value: string): number {
  return new TextEncoder().encode(value).length;
}

const dirtyForms = new Set<() => boolean>();
/** Set after the user agreed to discard all drafts (sign-out); skips the next guards. */
let discardConfirmed = false;

function askDiscard(): Promise<boolean> {
  return confirm({
    title: translate("ui.form.unsavedTitle"),
    description: translate("ui.form.unsavedDescription"),
    confirmLabel: translate("common.actions.discard"),
    cancelLabel: translate("common.actions.keepEditing"),
    tone: "danger",
  });
}

/**
 * Asks once whether to discard every unsaved draft on the page; for actions
 * that leave the page, such as signing out. Resolves `true` when nothing is
 * dirty or the user agreed.
 */
export async function confirmDiscardDrafts(): Promise<boolean> {
  if (![...dirtyForms].some((isDirty) => isDirty())) return true;
  discardConfirmed = await askDiscard();
  return discardConfirmed;
}

/**
 * Asks before leaving a page or closing the tab while `dirty` is true.
 * Route changes use ConfirmDialog; tab close uses the browser prompt.
 */
export function useDirtyGuard(dirty: MaybeRefOrGetter<boolean>): void {
  const isDirty = () => toValue(dirty);
  const guard = async () => {
    if (discardConfirmed || !isDirty()) return true;
    return askDiscard();
  };
  onBeforeRouteLeave(guard);
  onBeforeRouteUpdate(guard);
  const beforeUnload = (event: BeforeUnloadEvent) => {
    if (isDirty()) event.preventDefault();
  };
  onMounted(() => {
    dirtyForms.add(isDirty);
    window.addEventListener("beforeunload", beforeUnload);
  });
  onBeforeUnmount(() => {
    dirtyForms.delete(isDirty);
    discardConfirmed = false;
    window.removeEventListener("beforeunload", beforeUnload);
  });
}

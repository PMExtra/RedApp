import { onBeforeUnmount, onMounted, toValue, type MaybeRefOrGetter } from "vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate, type RouteLocationNormalized } from "vue-router";
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

/** Number of Unicode code points, for limits the server counts in characters. */
export function codePointLength(value: string): number {
  let length = 0;
  for (const _ of value) length++;
  return length;
}

/** UTF-8 byte length, for limits the server counts in bytes (passwords). */
export function utf8Length(value: string): number {
  return new TextEncoder().encode(value).length;
}

const dirtyForms = new Set<() => boolean>();
/**
 * Navigations currently let past the guards (see `leaveDiscardingDrafts`).
 * A counter rather than a flag: it only covers navigations awaited inside the
 * callback and drops back when they settle, whether they succeed or fail.
 */
let bypassing = 0;

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
 * Asks once whether to discard every unsaved draft on the page, before an
 * action that ends up leaving it (signing out, changing the password).
 * Resolves `true` when nothing is dirty or the user agreed. The answer is not
 * remembered: the navigation that follows the action uses
 * `leaveDiscardingDrafts`, so a failed action leaves the guards in place.
 */
export async function confirmDiscardDrafts(): Promise<boolean> {
  if (![...dirtyForms].some((isDirty) => isDirty())) return true;
  return askDiscard();
}

/**
 * Runs `navigate` (a `router.push`/`replace`) with the leave guards bypassed,
 * for navigations whose drafts were already confirmed or no longer mean
 * anything: the sign-in page after signing out or changing the password, the
 * list after deleting the edited vendor or application. The bypass ends when
 * the navigation settles, so later navigations ask again.
 */
export async function leaveDiscardingDrafts<T>(navigate: () => Promise<T>): Promise<T> {
  bypassing++;
  try {
    return await navigate();
  } finally {
    bypassing--;
  }
}

/**
 * The answer per navigation. Every dirty form on the page guards the same
 * navigation (vue-router passes the same `to` object to each guard), so a
 * page with several unsaved sections asks only once.
 */
const answers = new WeakMap<RouteLocationNormalized, Promise<boolean>>();

/**
 * Asks before leaving a page or closing the tab while `dirty` is true.
 * Route changes use ConfirmDialog (once per navigation, however many forms
 * are dirty); tab close uses the browser prompt.
 */
export function useDirtyGuard(dirty: MaybeRefOrGetter<boolean>): void {
  const isDirty = () => toValue(dirty);
  const guard = (to: RouteLocationNormalized) => {
    if (bypassing > 0 || !isDirty()) return true;
    let answer = answers.get(to);
    if (!answer) {
      answer = askDiscard();
      answers.set(to, answer);
    }
    return answer;
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
    window.removeEventListener("beforeunload", beforeUnload);
  });
}

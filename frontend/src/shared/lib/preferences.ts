import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { detectLocale, persistLocale, type Locale } from "@/shared/i18n";

export type ThemePreference = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

const THEME_KEY = "redapp-theme";

function loadTheme(): ThemePreference {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved === "light" || saved === "dark" || saved === "system") return saved;
  } catch {
    // Storage may be disabled.
  }
  return "system";
}

function systemQuery(): MediaQueryList | undefined {
  return typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : undefined;
}

/** UI preferences: pure client state, persisted in localStorage. */
export const usePreferencesStore = defineStore("preferences", () => {
  const locale = ref<Locale>(detectLocale());
  const theme = ref<ThemePreference>(loadTheme());
  const query = systemQuery();
  const systemDark = ref(query?.matches ?? false);
  query?.addEventListener("change", (event) => {
    systemDark.value = event.matches;
  });

  const resolvedTheme = computed<ResolvedTheme>(() =>
    theme.value === "system" ? (systemDark.value ? "dark" : "light") : theme.value,
  );

  function setLocale(next: Locale) {
    locale.value = next;
    persistLocale(next);
  }

  function setTheme(next: ThemePreference) {
    theme.value = next;
    try {
      localStorage.setItem(THEME_KEY, next);
    } catch {
      // The in-memory choice still applies.
    }
  }

  return { locale, theme, resolvedTheme, setLocale, setTheme };
});

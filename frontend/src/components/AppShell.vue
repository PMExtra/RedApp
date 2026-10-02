<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { language, setLanguage, t, type Language } from "../i18n";
import Icon from "./Icon.vue";
const info = ref<{ version: string; os: string; arch: string }>();
const failed = ref(false);
const controller = new AbortController();
onMounted(async () => {
  try {
    const response = await fetch("/api/info", {
      credentials: "omit",
      cache: "no-store",
      signal: controller.signal,
    });
    if (!response.ok) throw Error("info unavailable");
    info.value = await response.json();
  } catch (error) {
    if (!(error instanceof Error && error.name === "AbortError"))
      failed.value = true;
  }
});
onUnmounted(() => controller.abort());
</script>
<template>
  <div class="app-shell">
    <a class="skip-link" href="#main-content">{{ t("Skip to content") }}</a>
    <header class="topbar">
      <a href="/" class="brand"
        ><strong>RedApp</strong><span>Application Redistribution</span></a
      >
      <div class="topbar-actions">
        <label class="language-control"
          ><Icon name="globe" /><span class="sr-only">{{ t("Language") }}</span
          ><select
            :value="language"
            @change="
              setLanguage(
                ($event.target as HTMLSelectElement).value as Language,
              )
            "
            :aria-label="t('Language')"
          >
            <option value="en">English</option>
            <option value="zh-CN">简体中文</option>
          </select></label
        >
        <slot name="actions" />
      </div>
    </header>
    <div class="shell-content"><slot /></div>
    <footer class="app-footer">
      <span
        >RedApp
        <template v-if="info"
          ><span :aria-label="t('Version')">v{{ info.version }}</span></template
        ><span v-else>{{
          failed ? t("Version unavailable") : t("Loading…")
        }}</span></span
      ><span v-if="info" class="architecture"
        ><span>{{ t("Architecture") }}</span> {{ info.os }} /
        {{ info.arch }}</span
      >
    </footer>
  </div>
</template>

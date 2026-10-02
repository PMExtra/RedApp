<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { language, setLanguage, t, type Language } from "../i18n";
import SelectMenu from "./SelectMenu.vue";
import { applySite, siteRevision, siteSettings, siteTitle } from "../site";
const props = defineProps<{ admin?: boolean }>();
const info = ref<{ version: string; os: string; arch: string }>();
const failed = ref(false);
const controller = new AbortController();
onMounted(async () => {
  const loadedAt = siteRevision.value;
  try {
    const response = await fetch("/api/info", {
      credentials: "omit",
      cache: "no-store",
      signal: controller.signal,
    });
    if (!response.ok) throw Error("info unavailable");
    const data = await response.json();
    info.value = data;
    applySite(data.site, loadedAt);
  } catch (error) {
    if (!(error instanceof Error && error.name === "AbortError"))
      failed.value = true;
  }
});
onUnmounted(() => controller.abort());
</script>
<template>
  <div class="app-shell" :class="{ 'public-shell': !props.admin }">
    <a class="skip-link" href="#main-content">{{ t("Skip to content") }}</a>
    <header class="topbar">
      <a href="/" class="brand"
        ><strong>{{ siteTitle }}</strong
        ><span>{{ siteSettings.subtitle[language] }}</span></a
      >
      <div class="topbar-actions">
        <div class="language-control">
          <SelectMenu
            :model-value="language"
            @update:model-value="setLanguage($event as Language)"
            :label="t('Language')"
            :icon="true"
            :options="[
              { value: 'en', label: 'English' },
              { value: 'zh-CN', label: '简体中文' },
            ]"
          />
        </div>
        <slot name="actions" />
      </div>
    </header>
    <div class="shell-content"><slot /></div>
    <footer class="app-footer">
      <span class="project-version"
        ><a href="https://github.com/PMExtra/RedApp">RedApp</a>
        <span v-if="info" :aria-label="t('Version')"
          >v{{ info.version
          }}<template v-if="props.admin">
            ({{ info.os }}/{{ info.arch }})</template
          ></span
        >
        <span v-else>{{
          failed ? t("Version unavailable") : t("Loading…")
        }}</span>
      </span>
      <p v-if="siteSettings.disclaimer[language]" class="footer-notice">
        {{ siteSettings.disclaimer[language] }}
      </p>
    </footer>
  </div>
</template>

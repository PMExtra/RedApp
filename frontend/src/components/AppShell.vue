<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { inject } from "vue";
import { routerKey } from "vue-router";
const router = inject(routerKey, undefined);
import {
  bootstrap,
  bootstrapError,
  bootstrapLoading,
  loadBootstrap,
} from "../bootstrap";
import { language, setLanguage, t, type Language } from "../i18n";
import PublicSearch from "./PublicSearch.vue";
import SelectMenu from "./SelectMenu.vue";
import { siteSettings, siteTitle } from "../site";
const props = defineProps<{ admin?: boolean }>();
const info = bootstrap;
const failed = bootstrapError;
</script>
<template>
  <div class="app-shell" :class="{ 'public-shell': !props.admin }">
    <a class="skip-link" href="#main-content">{{ t("Skip to content") }}</a>
    <header class="topbar">
      <RouterLink to="/" class="brand"
        ><strong v-if="!bootstrapLoading || info || failed">{{
          siteTitle
        }}</strong
        ><span v-else class="brand-skeleton" :aria-label="t('Loading…')"></span
        ><span v-if="!bootstrapLoading || info || failed">{{
          siteSettings.subtitle[language]
        }}</span></RouterLink
      >
      <PublicSearch v-if="router" />
      <div class="topbar-actions">
        <div class="language-control">
          <SelectMenu topbar
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
    <div class="shell-content">
      <p v-if="failed" class="bootstrap-warning" role="status">
        {{ t("Site information unavailable.") }}
        <IconButton
          class="secondary"
          :disabled="bootstrapLoading"
          @click="loadBootstrap"
          icon="refresh"
          :label="t('Retry')"
        />
      </p>
      <slot />
    </div>
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
      <p
        v-if="
          (!bootstrapLoading || info || failed) &&
          siteSettings.disclaimer[language]
        "
        class="footer-notice"
      >
        {{ siteSettings.disclaimer[language] }}
      </p>
    </footer>
  </div>
</template>

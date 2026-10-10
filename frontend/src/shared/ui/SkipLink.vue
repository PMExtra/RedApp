<script setup lang="ts">
import { useI18n } from "vue-i18n";

/** First focusable element of each shell; targets `<main id="main-content" tabindex="-1">`. */
const { t } = useI18n();
</script>

<template>
  <a
    href="#main-content"
    class="sr-only z-tooltip rounded-md bg-surface-raised px-3 py-2 text-sm font-medium shadow-overlay focus:not-sr-only focus:fixed focus:start-3 focus:top-3"
  >
    {{ t("ui.nav.skipToContent") }}
  </a>
</template>

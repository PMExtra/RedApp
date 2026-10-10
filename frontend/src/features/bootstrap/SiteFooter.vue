<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useSiteTexts } from "./queries";

/** Footer with the disclaimer, source link and build version. */
const props = defineProps<{ showPlatform?: boolean }>();
const { t } = useI18n();
const { bootstrap, disclaimer } = useSiteTexts();
const version = computed(() => {
  const data = bootstrap.data.value;
  if (!data) return bootstrap.isError.value ? t("site.versionUnavailable") : "";
  return props.showPlatform
    ? t("site.versionPlatform", { version: data.version, platform: `${data.os}/${data.arch}` })
    : t("site.version", { version: data.version });
});
</script>

<template>
  <footer class="border-t border-border bg-surface">
    <div
      class="mx-auto flex max-w-7xl flex-col gap-2 px-4 py-6 text-xs text-muted sm:flex-row sm:items-start sm:justify-between sm:px-6"
    >
      <p v-if="disclaimer" class="max-w-3xl whitespace-pre-line">{{ disclaimer }}</p>
      <p class="flex shrink-0 items-center gap-3">
        <a
          href="https://github.com/PMExtra/RedApp"
          class="rounded-sm hover:text-fg hover:underline focus-ring"
          rel="noopener noreferrer"
          target="_blank"
        >
          {{ t("site.source") }}
        </a>
        <span v-if="version" class="tabular-nums">{{ version }}</span>
      </p>
    </div>
  </footer>
</template>

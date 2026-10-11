<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { Badge, type BadgeTone } from "@/shared/ui";

type Files = Schema<"VersionFiles">;

/** Stored generations of a version by state; retired copies as muted text. */
const props = defineProps<{ files: Files }>();
const { t } = useI18n();

const tones: [keyof Omit<Files, "retired">, BadgeTone][] = [
  ["failed", "danger"],
  ["interrupted", "warning"],
  ["in_progress", "info"],
  ["complete", "success"],
];
const badges = computed(() => tones.filter(([key]) => props.files[key] > 0));
</script>

<template>
  <span class="inline-flex flex-wrap items-center gap-1.5">
    <Badge v-for="[key, tone] in badges" :key="key" :tone="tone" class="whitespace-nowrap">
      {{ t(`releases.versions.summary.${key}`, { count: files[key] }) }}
    </Badge>
    <span v-if="files.retired > 0" class="text-xs whitespace-nowrap text-muted">
      {{ t("releases.versions.summary.retired", { count: files.retired }) }}
    </span>
    <span v-if="!badges.length && !files.retired" class="text-muted">
      {{ t("releases.versions.summary.none") }}
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { cn } from "@/shared/ui";
import type { DirectoryState } from "./queries";

/**
 * All / enabled / disabled toggle buttons. `deleted` has no button of its own;
 * it appears (pressed) only while the URL explicitly asks for it.
 */
const model = defineModel<DirectoryState>({ required: true });
const { t } = useI18n();
const options = computed(() => {
  const base: { value: DirectoryState; label: string }[] = [
    { value: "current", label: t("directory.state.all") },
    { value: "enabled", label: t("directory.state.enabled") },
    { value: "disabled", label: t("directory.state.disabled") },
  ];
  if (model.value === "deleted")
    base.push({ value: "deleted", label: t("directory.state.deleted") });
  return base;
});
</script>

<template>
  <div
    role="group"
    :aria-label="t('directory.state.label')"
    class="inline-flex rounded-md border border-border-strong bg-surface p-0.5 shadow-sm"
  >
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      :aria-pressed="model === option.value"
      :class="
        cn(
          'h-8 rounded-sm px-3 text-sm font-medium text-muted transition-colors hover:text-fg focus-ring',
          model === option.value && 'bg-primary-soft text-primary hover:text-primary',
        )
      "
      @click="model = option.value"
    >
      {{ option.label }}
    </button>
  </div>
</template>

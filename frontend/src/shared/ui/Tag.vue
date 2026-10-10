<script setup lang="ts">
import { X } from "@lucide/vue";
import { useI18n } from "vue-i18n";

/** A label chip, optionally removable (tag and category editors). */
defineProps<{ label: string; removable?: boolean; disabled?: boolean }>();
const emit = defineEmits<{ remove: [] }>();
const { t } = useI18n();
</script>

<template>
  <span
    class="inline-flex items-center gap-1 rounded-md border border-border bg-surface-sunken py-0.5 ps-2 text-xs"
    :class="removable ? 'pe-0.5' : 'pe-2'"
  >
    {{ label }}
    <button
      v-if="removable"
      type="button"
      :disabled="disabled"
      :aria-label="t('ui.tag.remove', { label })"
      class="rounded-sm p-0.5 text-muted hover:bg-surface-hover hover:text-fg focus-ring disabled:opacity-50"
      @click="emit('remove')"
    >
      <X class="size-3" aria-hidden="true" />
    </button>
  </span>
</template>

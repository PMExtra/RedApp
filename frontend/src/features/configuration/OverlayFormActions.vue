<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Button } from "@/shared/ui";

/**
 * Footer of a configuration form: save (submit), discard and the draft state.
 * Place it inside the `<form>`; `form` targets a form elsewhere on the page.
 */
defineProps<{ dirty: boolean; saving?: boolean; disabled?: boolean; form?: string }>();
const emit = defineEmits<{ discard: [] }>();
const { t } = useI18n();
</script>

<template>
  <div class="flex flex-wrap items-center justify-end gap-3">
    <p class="me-auto text-sm text-muted" aria-live="polite">
      {{ dirty ? t("configuration.unsaved") : "" }}
    </p>
    <Button :disabled="disabled || saving || !dirty" @click="emit('discard')">
      {{ t("configuration.discard") }}
    </Button>
    <Button type="submit" variant="primary" :form="form" :loading="saving" :disabled="disabled">
      {{ t("common.actions.save") }}
    </Button>
  </div>
</template>

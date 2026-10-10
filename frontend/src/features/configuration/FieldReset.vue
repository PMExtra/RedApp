<script setup lang="ts">
import { computed } from "vue";
import { RotateCcw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { IconButton } from "@/shared/ui";

/**
 * "Restore template value" next to an overlay field. Visible only for
 * template-linked entities when the field is overridden (`custom`) or edited.
 * The caller restores the draft and adds the path to `unset` when it was custom.
 */
const props = defineProps<{
  /** `configuration.template_ref !== null`. */
  linked: boolean;
  origin?: Schema<"FieldOrigin">;
  /** The draft differs from the saved value. */
  modified?: boolean;
  /** Template default shown in the tooltip. */
  templateValue?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ reset: [] }>();
const { t } = useI18n();
const visible = computed(
  () => props.linked && (props.modified || props.origin?.source === "custom"),
);
const label = computed(() =>
  props.templateValue
    ? t("configuration.resetTo", { value: props.templateValue })
    : t("configuration.reset"),
);
</script>

<template>
  <IconButton v-if="visible" :label="label" size="sm" :disabled="disabled" @click="emit('reset')">
    <RotateCcw aria-hidden="true" />
  </IconButton>
</template>

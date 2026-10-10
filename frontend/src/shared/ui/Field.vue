<script setup lang="ts">
import { computed, useId } from "vue";
import { useI18n } from "vue-i18n";
import type { FieldControlProps } from "./types";

/**
 * Label, description and error around one control, without form state.
 * Bind the slot's `control` to the focusable element: `<Input v-bind="control" />`.
 * Use FormField inside vee-validate forms.
 */
const props = defineProps<{
  label: string;
  description?: string;
  error?: string;
  required?: boolean;
  id?: string;
  /** Hide the label visually (keep it for screen readers). */
  hideLabel?: boolean;
}>();
defineSlots<{ default: (props: { control: FieldControlProps }) => unknown }>();
const { t } = useI18n();
const generated = useId();
const id = computed(() => props.id ?? generated);
const control = computed<FieldControlProps>(() => ({
  id: id.value,
  "aria-labelledby": `${id.value}-label`,
  "aria-describedby":
    [props.description && `${id.value}-description`, props.error && `${id.value}-error`]
      .filter(Boolean)
      .join(" ") || undefined,
  "aria-invalid": props.error ? "true" : undefined,
  "aria-required": props.required ? "true" : undefined,
}));
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label
      :id="`${id}-label`"
      :for="id"
      class="text-sm font-medium"
      :class="hideLabel && 'sr-only'"
    >
      {{ label }}
      <span v-if="required" class="text-danger" aria-hidden="true">*</span>
      <span v-if="required" class="sr-only">({{ t("ui.form.required") }})</span>
    </label>
    <slot :control="control" />
    <p v-if="description" :id="`${id}-description`" class="text-xs text-muted">
      {{ description }}
    </p>
    <p v-if="error" :id="`${id}-error`" class="text-xs text-danger">{{ error }}</p>
  </div>
</template>

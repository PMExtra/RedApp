<script setup lang="ts">
import { computed } from "vue";
import { useField } from "vee-validate";
import { displayFormError } from "./forms";
import Field from "@/shared/ui/Field.vue";
import type { FieldControlProps, FormFieldBinding } from "@/shared/ui/types";

/**
 * A vee-validate field with label, description and error message:
 * `<FormField v-slot="{ field }" name="title" :label="t('…')"><Input v-bind="field" /></FormField>`.
 * Errors appear after the field is touched or the form was submitted.
 */
const props = defineProps<{
  name: string;
  label: string;
  description?: string;
  required?: boolean;
  hideLabel?: boolean;
}>();
defineSlots<{ default: (props: { field: FormFieldBinding }) => unknown }>();

const { value, errorMessage, handleBlur, meta, setValue } = useField(() => props.name);
const shownError = computed(() =>
  meta.touched || meta.validated ? displayFormError(errorMessage.value) : undefined,
);

function binding(control: FieldControlProps): FormFieldBinding {
  return {
    ...control,
    name: props.name,
    modelValue: value.value,
    "onUpdate:modelValue": (next: unknown) => {
      setValue(next, meta.touched);
    },
    onBlur: () => {
      handleBlur(undefined, true);
    },
  };
}
</script>

<template>
  <Field
    v-slot="{ control }"
    :label="label"
    :description="description"
    :error="shownError"
    :required="required"
    :hide-label="hideLabel"
  >
    <slot :field="binding(control)" />
  </Field>
</template>

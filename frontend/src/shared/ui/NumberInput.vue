<script setup lang="ts">
import { Minus, Plus } from "@lucide/vue";
import {
  NumberFieldDecrement,
  NumberFieldIncrement,
  NumberFieldInput,
  NumberFieldRoot,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import { controlClass, cn } from "./cn";

defineOptions({ inheritAttrs: false });
/** `null` when the field is empty. */
const model = defineModel<number | null>({ default: null });
defineProps<{
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
  name?: string;
  /** Unit shown after the field, e.g. "s" or "MiB". */
  unit?: string;
}>();
const { t, locale } = useI18n();
const stepper =
  "flex w-8 items-center justify-center text-muted hover:text-fg disabled:opacity-40 focus-ring";
</script>

<template>
  <div class="flex items-center gap-2">
    <NumberFieldRoot
      :model-value="model"
      :min="min"
      :max="max"
      :step="step"
      :disabled="disabled"
      :name="name"
      :locale="locale"
      :format-options="{ useGrouping: false }"
      :class="cn(controlClass, 'flex h-9 items-stretch p-0')"
      @update:model-value="(value: number | undefined) => (model = value ?? null)"
    >
      <NumberFieldDecrement :class="stepper" :aria-label="t('ui.numberInput.decrement')">
        <Minus class="size-4" aria-hidden="true" />
      </NumberFieldDecrement>
      <NumberFieldInput
        v-bind="$attrs"
        class="w-full min-w-0 bg-transparent text-center tabular-nums outline-none"
      />
      <NumberFieldIncrement :class="stepper" :aria-label="t('ui.numberInput.increment')">
        <Plus class="size-4" aria-hidden="true" />
      </NumberFieldIncrement>
    </NumberFieldRoot>
    <span v-if="unit" class="text-sm text-muted">{{ unit }}</span>
  </div>
</template>

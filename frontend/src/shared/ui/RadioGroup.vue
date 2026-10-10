<script setup lang="ts">
import { useId } from "vue";
import { RadioGroupIndicator, RadioGroupItem, RadioGroupRoot } from "reka-ui";
import type { RadioOption } from "./types";

defineOptions({ inheritAttrs: false });
const model = defineModel<string | undefined>();
withDefaults(
  defineProps<{
    options: RadioOption[];
    disabled?: boolean;
    name?: string;
    orientation?: "vertical" | "horizontal";
  }>(),
  { orientation: "vertical" },
);
const base = useId();
</script>

<template>
  <RadioGroupRoot
    v-bind="$attrs"
    :model-value="model"
    :disabled="disabled"
    :name="name"
    :orientation="orientation"
    :class="orientation === 'vertical' ? 'flex flex-col gap-2' : 'flex flex-wrap gap-4'"
    @update:model-value="
      (value: unknown) => (model = typeof value === 'string' ? value : undefined)
    "
  >
    <div v-for="option in options" :key="option.value" class="flex items-start gap-2">
      <RadioGroupItem
        :id="`${base}-${option.value}`"
        :value="option.value"
        :disabled="option.disabled"
        class="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border border-border-strong bg-surface focus-ring disabled:opacity-50 data-[state=checked]:border-primary"
      >
        <RadioGroupIndicator class="block size-2 rounded-full bg-primary" />
      </RadioGroupItem>
      <label :for="`${base}-${option.value}`" class="text-sm">
        {{ option.label }}
        <span v-if="option.description" class="block text-xs text-muted">
          {{ option.description }}
        </span>
      </label>
    </div>
  </RadioGroupRoot>
</template>

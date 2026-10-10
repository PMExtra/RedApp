<script setup lang="ts">
import { useId } from "vue";
import { Check, Minus } from "@lucide/vue";
import { CheckboxIndicator, CheckboxRoot } from "reka-ui";

defineOptions({ inheritAttrs: false });
const model = defineModel<boolean | "indeterminate">({ default: false });
const props = defineProps<{ id?: string; label?: string; disabled?: boolean; name?: string }>();
const fallbackId = useId();
</script>

<template>
  <span class="inline-flex items-center gap-2">
    <CheckboxRoot
      v-bind="$attrs"
      :id="props.id ?? fallbackId"
      v-model="model"
      :disabled="disabled"
      :name="name"
      class="flex size-4 shrink-0 items-center justify-center rounded-sm border border-border-strong bg-surface text-primary-fg focus-ring disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=indeterminate]:border-primary data-[state=indeterminate]:bg-primary"
    >
      <CheckboxIndicator>
        <Minus v-if="model === 'indeterminate'" class="size-3" aria-hidden="true" />
        <Check v-else class="size-3" aria-hidden="true" />
      </CheckboxIndicator>
    </CheckboxRoot>
    <label v-if="label || $slots.default" :for="props.id ?? fallbackId" class="text-sm">
      <slot>{{ label }}</slot>
    </label>
  </span>
</template>

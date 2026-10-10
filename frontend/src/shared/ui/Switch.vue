<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from "reka-ui";

defineOptions({ inheritAttrs: false });
const model = defineModel<boolean>({ default: false });
defineProps<{ disabled?: boolean; name?: string }>();
</script>

<template>
  <SwitchRoot
    v-bind="$attrs"
    v-model="model"
    :disabled="disabled"
    :name="name"
    class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent bg-border-strong transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary"
  >
    <SwitchThumb
      class="pointer-events-none block size-4 rounded-full bg-surface shadow-sm transition-transform data-[state=checked]:translate-x-4 rtl:data-[state=checked]:-translate-x-4"
    />
  </SwitchRoot>
</template>

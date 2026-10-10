<script setup lang="ts">
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from "reka-ui";
import { cn, panelClass } from "./cn";

const open = defineModel<boolean>("open", { default: false });
withDefaults(
  defineProps<{
    side?: "top" | "right" | "bottom" | "left";
    align?: "start" | "center" | "end";
    /** Accessible name of the panel when it has no visible heading. */
    label?: string;
  }>(),
  { side: "bottom", align: "start" },
);
defineSlots<{ trigger: () => unknown; default: () => unknown }>();
</script>

<template>
  <PopoverRoot v-model:open="open">
    <PopoverTrigger as-child>
      <slot name="trigger" />
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent
        :side="side"
        :align="align"
        :side-offset="6"
        :aria-label="label"
        :class="cn(panelClass, 'max-w-sm p-3 focus:outline-none')"
      >
        <slot />
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>

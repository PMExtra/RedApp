<script setup lang="ts">
import { TooltipContent, TooltipPortal, TooltipRoot, TooltipTrigger } from "reka-ui";

withDefaults(
  defineProps<{
    content: string;
    side?: "top" | "right" | "bottom" | "left";
  }>(),
  { side: "top" },
);
</script>

<template>
  <TooltipRoot>
    <TooltipTrigger as-child>
      <slot />
    </TooltipTrigger>
    <TooltipPortal>
      <TooltipContent
        :side="side"
        :side-offset="6"
        class="z-tooltip max-w-xs rounded-md bg-fg px-2 py-1 text-xs text-inverse shadow-md"
      >
        {{ content }}
      </TooltipContent>
    </TooltipPortal>
  </TooltipRoot>
</template>

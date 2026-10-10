<script setup lang="ts">
import { Info } from "@lucide/vue";
import { cn } from "./cn";
import Tooltip from "./Tooltip.vue";

/**
 * A small info icon whose tooltip carries explanatory text, so labels can stay
 * short. The tooltip opens on hover and on keyboard focus.
 */
defineProps<{
  /** Accessible name of the icon, for example "About Free space". */
  label: string;
  content: string;
  class?: string;
}>();
</script>

<template>
  <Tooltip :content="content">
    <button
      type="button"
      :aria-label="label"
      :class="
        cn(
          'inline-flex size-5 shrink-0 items-center justify-center rounded-full text-subtle hover:text-fg focus-ring',
          $props.class,
        )
      "
    >
      <Info class="size-3.5" aria-hidden="true" />
    </button>
  </Tooltip>
</template>

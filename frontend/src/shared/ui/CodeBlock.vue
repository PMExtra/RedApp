<script setup lang="ts">
import CopyButton from "./CopyButton.vue";

/** Preformatted code or a command with a copy button. */
defineProps<{ code: string; label?: string }>();
</script>

<template>
  <figure class="overflow-hidden rounded-lg border border-border bg-surface-sunken">
    <figcaption
      v-if="label"
      class="flex items-center justify-between border-b border-border px-3 py-1 text-xs text-muted"
    >
      <span>{{ label }}</span>
      <CopyButton :text="code" />
    </figcaption>
    <div class="relative">
      <pre
        class="overflow-x-auto p-3 pe-12 text-[0.8125rem] leading-relaxed"
      ><code>{{ code }}</code></pre>
      <CopyButton v-if="!label" :text="code" class="absolute end-1.5 top-1.5" />
    </div>
  </figure>
</template>

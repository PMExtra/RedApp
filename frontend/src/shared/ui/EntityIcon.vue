<script setup lang="ts">
import { ref, watch } from "vue";
import { Box } from "@lucide/vue";
import { cn } from "./cn";

/**
 * Vendor or application icon from an `IconPath` (`""` for none). Square icons
 * fall back to a generic glyph when missing or broken; `logo` keeps the aspect
 * ratio at a fixed height and renders nothing without an image. Icons are
 * drawn for light backgrounds: the light theme shows them on the page itself,
 * the dark theme on a dimmed light tile.
 */
const props = withDefaults(
  defineProps<{
    src: string;
    /** Empty for decorative icons next to a visible name. */
    alt?: string;
    size?: "xs" | "sm" | "md" | "lg" | "xl";
    variant?: "square" | "logo";
  }>(),
  { alt: "", size: "md", variant: "square" },
);
const failed = ref(false);
watch(
  () => props.src,
  () => {
    failed.value = false;
  },
);
const squares = { xs: "size-5", sm: "size-7", md: "size-9", lg: "size-12", xl: "size-16" };
const heights = { xs: "h-5", sm: "h-7", md: "h-9", lg: "h-12", xl: "h-16" };
</script>

<template>
  <template v-if="variant === 'logo'">
    <img
      v-if="src && !failed"
      :src="src"
      :alt="alt"
      :class="
        cn(
          'w-auto max-w-48 rounded-lg bg-icon-bg object-contain dark:box-content dark:px-2 dark:py-1',
          heights[size],
        )
      "
      @error="failed = true"
    />
  </template>
  <span
    v-else
    :class="
      cn(
        'inline-flex shrink-0 items-center justify-center overflow-hidden rounded-lg bg-icon-bg',
        squares[size],
      )
    "
  >
    <img
      v-if="src && !failed"
      :src="src"
      :alt="alt"
      class="size-full object-contain dark:p-[12%]"
      @error="failed = true"
    />
    <Box
      v-else
      class="size-3/5 text-subtle dark:text-[#6b7280]"
      :aria-label="alt || undefined"
      :aria-hidden="!alt"
    />
  </span>
</template>

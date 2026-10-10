<script setup lang="ts">
import { DropdownMenuItem } from "reka-ui";
import { cn, itemClass } from "./cn";

defineProps<{ disabled?: boolean; tone?: "default" | "danger"; asChild?: boolean }>();
const emit = defineEmits<{ select: [event: Event] }>();
</script>

<template>
  <DropdownMenuItem
    :disabled="disabled"
    :as-child="asChild"
    :class="cn(itemClass, '[&_svg]:size-4', tone === 'danger' && 'text-danger')"
    @select="emit('select', $event)"
  >
    <slot />
  </DropdownMenuItem>
</template>

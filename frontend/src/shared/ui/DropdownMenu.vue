<script setup lang="ts">
import {
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from "reka-ui";
import { cn, panelClass } from "./cn";

/**
 * Menu of actions. Put DropdownMenuItem (and Reka's DropdownMenuSeparator,
 * DropdownMenuLabel, DropdownMenuRadioGroup) in the default slot.
 */
withDefaults(
  defineProps<{
    align?: "start" | "center" | "end";
    /** Extra classes of the menu panel, for example a maximum height. */
    contentClass?: string;
  }>(),
  { align: "end", contentClass: undefined },
);
defineSlots<{ trigger: () => unknown; default: () => unknown }>();
</script>

<template>
  <DropdownMenuRoot :modal="false">
    <DropdownMenuTrigger as-child>
      <slot name="trigger" />
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent :align="align" :side-offset="6" :class="cn(panelClass, contentClass)">
        <slot />
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import {
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from "reka-ui";
import { panelClass } from "./cn";

/**
 * Menu of actions. Put DropdownMenuItem (and Reka's DropdownMenuSeparator,
 * DropdownMenuLabel, DropdownMenuRadioGroup) in the default slot.
 */
withDefaults(defineProps<{ align?: "start" | "center" | "end" }>(), { align: "end" });
defineSlots<{ trigger: () => unknown; default: () => unknown }>();
</script>

<template>
  <DropdownMenuRoot :modal="false">
    <DropdownMenuTrigger as-child>
      <slot name="trigger" />
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent :align="align" :side-offset="6" :class="panelClass">
        <slot />
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import { X } from "@lucide/vue";
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
  DialogTrigger,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import { cn } from "./cn";

const open = defineModel<boolean>("open", { default: false });
withDefaults(
  defineProps<{
    title: string;
    description?: string;
    size?: "sm" | "md" | "lg" | "xl";
    /** Prevent closing by Escape or outside click (for example while saving). */
    persistent?: boolean;
  }>(),
  { size: "md" },
);
defineSlots<{
  trigger?: () => unknown;
  default?: () => unknown;
  footer?: () => unknown;
}>();
const { t } = useI18n();
const sizes = { sm: "max-w-sm", md: "max-w-lg", lg: "max-w-2xl", xl: "max-w-4xl" };
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogTrigger v-if="$slots.trigger" as-child>
      <slot name="trigger" />
    </DialogTrigger>
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-overlay bg-overlay" />
      <DialogContent
        :class="
          cn(
            'fixed top-1/2 left-1/2 z-modal flex max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-xl border border-border bg-surface-raised shadow-overlay focus:outline-none',
            sizes[size],
          )
        "
        @escape-key-down="persistent && $event.preventDefault()"
        @pointer-down-outside="persistent && $event.preventDefault()"
        @interact-outside="persistent && $event.preventDefault()"
      >
        <div class="flex items-start justify-between gap-4 px-6 pt-5">
          <div class="flex flex-col gap-1">
            <DialogTitle class="text-lg font-semibold">{{ title }}</DialogTitle>
            <DialogDescription v-if="description" class="text-sm text-muted">
              {{ description }}
            </DialogDescription>
          </div>
          <DialogClose
            v-if="!persistent"
            :aria-label="t('ui.dialog.close')"
            class="-me-2 rounded-md p-1 text-muted hover:bg-surface-hover hover:text-fg focus-ring"
          >
            <X class="size-4" aria-hidden="true" />
          </DialogClose>
        </div>
        <div class="min-h-0 overflow-y-auto px-6 py-4">
          <slot />
        </div>
        <div
          v-if="$slots.footer"
          class="flex flex-wrap justify-end gap-2 border-t border-border px-6 py-3"
        >
          <slot name="footer" />
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

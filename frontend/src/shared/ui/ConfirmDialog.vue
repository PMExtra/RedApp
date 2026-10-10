<script setup lang="ts">
import {
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogOverlay,
  AlertDialogPortal,
  AlertDialogRoot,
  AlertDialogTitle,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import Button from "./Button.vue";

/**
 * Modal confirmation. Prefer the promise API `confirm()` from `@/shared/lib`,
 * rendered by ConfirmHost; use this component directly only for custom bodies.
 * Escape and Cancel both emit `cancel`; focus starts on Cancel.
 */
const open = defineModel<boolean>("open", { default: false });
const props = withDefaults(
  defineProps<{
    title: string;
    description?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    tone?: "default" | "danger";
  }>(),
  { tone: "default" },
);
const emit = defineEmits<{ confirm: []; cancel: [] }>();
const { t } = useI18n();

function onConfirm() {
  open.value = false;
  emit("confirm");
}

function onOpenChange(value: boolean) {
  open.value = value;
  if (!value) emit("cancel");
}
</script>

<template>
  <AlertDialogRoot :open="open" @update:open="onOpenChange">
    <AlertDialogPortal>
      <AlertDialogOverlay class="fixed inset-0 z-overlay bg-overlay" />
      <AlertDialogContent
        class="fixed top-1/2 left-1/2 z-modal flex w-[calc(100vw-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 flex-col gap-4 rounded-xl border border-border bg-surface-raised p-6 shadow-overlay focus:outline-none"
      >
        <div class="flex flex-col gap-2">
          <AlertDialogTitle class="text-lg font-semibold">{{ props.title }}</AlertDialogTitle>
          <AlertDialogDescription v-if="props.description" class="text-sm text-muted">
            {{ props.description }}
          </AlertDialogDescription>
          <slot />
        </div>
        <div class="flex flex-wrap justify-end gap-2">
          <AlertDialogCancel as-child>
            <Button>{{ props.cancelLabel ?? t("common.actions.cancel") }}</Button>
          </AlertDialogCancel>
          <!-- Not AlertDialogAction: closing through it would also emit cancel. -->
          <Button :variant="props.tone === 'danger' ? 'danger' : 'primary'" @click="onConfirm">
            {{ props.confirmLabel ?? t("common.actions.confirm") }}
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>

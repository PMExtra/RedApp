<script setup lang="ts">
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from "@lucide/vue";
import {
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastRoot,
  ToastTitle,
  ToastViewport,
} from "reka-ui";
import { useI18n } from "vue-i18n";
import { activeToasts, dismissToast, type ToastTone } from "@/shared/lib/toast";
import { cn } from "./cn";
import CopyButton from "./CopyButton.vue";

/** Renders `toast()` notifications; mounted once by each app shell. */
const toasts = activeToasts();
const { t } = useI18n();
const icons = { info: Info, success: CircleCheck, warning: TriangleAlert, error: CircleAlert };
const tones: Record<ToastTone, string> = {
  info: "text-info",
  success: "text-success",
  warning: "text-warning",
  error: "text-danger",
};
</script>

<template>
  <ToastProvider :label="t('ui.toast.region')" swipe-direction="right">
    <ToastRoot
      v-for="item in toasts"
      :key="item.id"
      :duration="item.duration"
      :type="item.tone === 'error' ? 'foreground' : 'background'"
      class="flex w-full items-start gap-3 rounded-lg border border-border bg-surface-raised p-3 shadow-overlay"
      @update:open="(open: boolean) => !open && dismissToast(item.id)"
    >
      <component
        :is="icons[item.tone]"
        :class="cn('mt-0.5 size-5 shrink-0', tones[item.tone])"
        aria-hidden="true"
      />
      <div class="flex min-w-0 flex-1 flex-col gap-1">
        <ToastTitle class="text-sm font-medium">{{ item.title }}</ToastTitle>
        <ToastDescription
          v-if="item.description || item.requestId"
          class="flex flex-col gap-1 text-xs text-muted"
        >
          <span v-if="item.description" class="break-words">{{ item.description }}</span>
          <span v-if="item.requestId" class="flex items-center gap-1">
            {{ t("common.requestId") }}:
            <code class="select-all">{{ item.requestId }}</code>
            <CopyButton :text="item.requestId" :label="t('common.requestId')" />
          </span>
        </ToastDescription>
      </div>
      <ToastClose
        :aria-label="t('ui.toast.dismiss')"
        class="rounded-md p-1 text-muted hover:bg-surface-hover hover:text-fg focus-ring"
      >
        <X class="size-4" aria-hidden="true" />
      </ToastClose>
    </ToastRoot>
    <ToastViewport
      class="fixed end-0 bottom-0 z-toast flex w-full max-w-sm flex-col gap-2 p-4 outline-none"
    />
  </ToastProvider>
</template>

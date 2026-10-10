<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { Check, Copy } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { toast } from "@/shared/lib/toast";
import Button from "./Button.vue";

const props = withDefaults(
  defineProps<{ text: string; label?: string; size?: "sm" | "md"; showLabel?: boolean }>(),
  { size: "sm" },
);
const { t } = useI18n();
const copied = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;

async function copy() {
  try {
    await navigator.clipboard.writeText(props.text);
    copied.value = true;
    clearTimeout(timer);
    timer = setTimeout(() => (copied.value = false), 2000);
  } catch {
    toast({ tone: "error", title: t("ui.copy.failed") });
  }
}
onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <Button
    variant="ghost"
    :size="size"
    :icon="!showLabel"
    :aria-label="showLabel ? undefined : (label ?? t('ui.copy.copy'))"
    @click="copy"
  >
    <Check v-if="copied" aria-hidden="true" class="text-success" />
    <Copy v-else aria-hidden="true" />
    <span v-if="showLabel">{{ copied ? t("ui.copy.copied") : (label ?? t("ui.copy.copy")) }}</span>
    <span class="sr-only" aria-live="polite">{{ copied ? t("ui.copy.copied") : "" }}</span>
  </Button>
</template>

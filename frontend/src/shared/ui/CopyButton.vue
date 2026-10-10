<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from "vue";
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
// The live region sits next to the button (not inside its name) and is
// emptied before each announcement, so copying again is announced again.
const announcement = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;

async function copy() {
  try {
    await navigator.clipboard.writeText(props.text);
    copied.value = true;
    announcement.value = "";
    await nextTick();
    announcement.value = t("ui.copy.copied");
    clearTimeout(timer);
    timer = setTimeout(() => {
      copied.value = false;
      announcement.value = "";
    }, 2000);
  } catch {
    toast({ tone: "error", title: t("ui.copy.failed") });
  }
}
onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <span class="inline-flex">
    <Button
      variant="ghost"
      :size="size"
      :icon="!showLabel"
      :aria-label="showLabel ? undefined : (label ?? t('ui.copy.copy'))"
      @click="copy"
    >
      <Check v-if="copied" aria-hidden="true" class="text-success" />
      <Copy v-else aria-hidden="true" />
      <span v-if="showLabel">{{
        copied ? t("ui.copy.copied") : (label ?? t("ui.copy.copy"))
      }}</span>
    </Button>
    <span class="sr-only" role="status">{{ announcement }}</span>
  </span>
</template>

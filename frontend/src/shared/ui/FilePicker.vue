<script setup lang="ts">
import { computed, ref } from "vue";
import { Upload, X } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import Button from "./Button.vue";

/**
 * File selection by button or drag and drop. Emits the chosen files; the
 * caller uploads them (see `uploadWithProgress`) and shows a ProgressBar.
 */
defineOptions({ inheritAttrs: false });
const files = defineModel<File[]>({ default: () => [] });
const props = defineProps<{ accept?: string; multiple?: boolean; disabled?: boolean }>();
const { t } = useI18n();
const format = useFormat();
const input = ref<HTMLInputElement>();
const dragging = ref(false);

const summary = computed(() =>
  files.value.map((file) => `${file.name} (${format.bytes(file.size)})`).join(", "),
);

function accepts(file: File): boolean {
  if (!props.accept) return true;
  const name = file.name.toLowerCase();
  return props.accept.split(",").some((pattern) => {
    const rule = pattern.trim().toLowerCase();
    if (rule.startsWith(".")) return name.endsWith(rule);
    if (rule.endsWith("/*")) return file.type.startsWith(rule.slice(0, -1));
    return file.type === rule;
  });
}

function choose(list: FileList | null | undefined) {
  const chosen = [...(list ?? [])].filter(accepts);
  if (chosen.length === 0) return;
  files.value = props.multiple ? chosen : chosen.slice(0, 1);
}

function onDrop(event: DragEvent) {
  dragging.value = false;
  if (!props.disabled) choose(event.dataTransfer?.files);
}

function clear() {
  files.value = [];
  if (input.value) input.value.value = "";
}
</script>

<template>
  <div
    class="flex flex-col gap-2 rounded-lg border border-dashed p-4 transition-colors"
    :class="dragging ? 'border-primary bg-primary-soft' : 'border-border-strong'"
    @dragover.prevent="dragging = !disabled"
    @dragleave="dragging = false"
    @drop.prevent="onDrop"
  >
    <div class="flex flex-wrap items-center gap-3">
      <input
        ref="input"
        v-bind="$attrs"
        type="file"
        class="sr-only"
        tabindex="-1"
        :accept="accept"
        :multiple="multiple"
        :disabled="disabled"
        @change="choose(($event.target as HTMLInputElement).files)"
      />
      <Button :disabled="disabled" @click="input?.click()">
        <Upload aria-hidden="true" />
        {{ multiple ? t("ui.filePicker.chooseMany") : t("ui.filePicker.choose") }}
      </Button>
      <span class="text-sm text-muted">
        {{ multiple ? t("ui.filePicker.dropMany") : t("ui.filePicker.drop") }}
      </span>
    </div>
    <div class="flex items-center gap-2 text-sm" aria-live="polite">
      <span class="min-w-0 truncate">{{ summary || t("ui.filePicker.none") }}</span>
      <Button
        v-if="files.length > 0"
        variant="ghost"
        size="sm"
        icon
        :disabled="disabled"
        :aria-label="t('ui.filePicker.clear')"
        @click="clear"
      >
        <X aria-hidden="true" />
      </Button>
    </div>
  </div>
</template>

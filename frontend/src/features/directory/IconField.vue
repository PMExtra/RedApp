<script setup lang="ts">
import { ref, useId } from "vue";
import { ImageUp, Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { notifyError } from "@/shared/lib";
import { Button, EntityIcon, ProgressBar } from "@/shared/ui";
import { uploadIcon } from "./queries";

/**
 * One icon slot (an `IconPath`): preview, upload and remove. The upload is
 * stored at once (content-addressed); the path is saved with the form.
 */
const model = defineModel<string>({ required: true });
const props = defineProps<{
  label: string;
  /** Shown instead of a preview when the slot is empty. */
  emptyText: string;
  variant?: "square" | "logo";
  disabled?: boolean;
}>();
defineSlots<{ reset?: () => unknown }>();
const { t } = useI18n();
const id = useId();
const input = ref<HTMLInputElement>();
const progress = ref<number | null>(null);
const uploading = ref(false);
const ACCEPT = "image/png,image/jpeg,image/svg+xml,.png,.jpg,.jpeg,.svg";

async function choose(event: Event): Promise<void> {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  target.value = "";
  if (!file || props.disabled) return;
  uploading.value = true;
  progress.value = null;
  try {
    model.value = await uploadIcon(file, {
      onProgress: ({ loaded, total }) => {
        progress.value = total ? Math.round((loaded / total) * 100) : null;
      },
    });
  } catch (error) {
    notifyError(error);
  } finally {
    uploading.value = false;
  }
}
</script>

<template>
  <div role="group" :aria-labelledby="`${id}-label`" class="flex flex-col gap-2">
    <div class="flex items-center justify-between gap-2">
      <span :id="`${id}-label`" class="text-sm font-medium">{{ label }}</span>
      <slot name="reset" />
    </div>
    <div
      class="flex h-20 items-center justify-center rounded-lg border border-border bg-surface-sunken p-2"
    >
      <EntityIcon v-if="model" :src="model" :variant="variant ?? 'square'" :alt="label" size="xl" />
      <span v-else class="text-xs text-muted">{{ emptyText }}</span>
    </div>
    <ProgressBar v-if="uploading" :value="progress" :label="t('directory.icon.uploading')" />
    <div class="flex flex-wrap gap-2">
      <input
        ref="input"
        type="file"
        class="sr-only"
        tabindex="-1"
        :accept="ACCEPT"
        :aria-label="`${label}: ${t('directory.icon.choose')}`"
        :disabled="disabled || uploading"
        @change="choose"
      />
      <Button
        size="sm"
        :disabled="disabled"
        :loading="uploading"
        :aria-label="`${label}: ${model ? t('directory.icon.replace') : t('directory.icon.choose')}`"
        @click="input?.click()"
      >
        <ImageUp aria-hidden="true" />
        {{ model ? t("directory.icon.replace") : t("directory.icon.choose") }}
      </Button>
      <Button
        v-if="model"
        size="sm"
        variant="ghost"
        :disabled="disabled || uploading"
        :aria-label="`${label}: ${t('directory.icon.remove')}`"
        @click="model = ''"
      >
        <Trash2 aria-hidden="true" />
        {{ t("directory.icon.remove") }}
      </Button>
    </div>
    <p class="text-xs text-muted">{{ t("directory.icon.hint") }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  isEmptyPatch,
  TemplateMissingAlert,
  useOverlayForm,
} from "@/features/configuration";
import { formError, FormField, utf8Length } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Dialog, Input, RevisionConflictAlert } from "@/shared/ui";
import { useCategory, useCategoryPatch, type CategoryPatch } from "./queries";

/**
 * Renames a category. Built-in categories can restore their template names;
 * names must be unique across categories (checked by the server).
 */
const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ category: string }>();
const { t } = useI18n();
const PATHS = ["name.en", "name.zh-CN"] as const;
const languages = ["en", "zh-CN"] as const;
const nameText = z
  .string()
  .refine((value) => value.trim() !== "", formError("taxonomy.edit.nameRequired"))
  .refine((value) => utf8Length(value) <= 256, formError("taxonomy.edit.nameTooLong"));
const schema = z.object({ name: z.object({ en: nameText, "zh-CN": nameText }) });

const query = useCategory(() => props.category, open);
const overlay = useOverlayForm({ configuration: query.data, paths: PATHS, schema });
const save = useCategoryPatch(() => props.category, query.data);
const { form } = overlay;
const title = computed(() => t("taxonomy.edit.title", { id: props.category }));

// Opening another category starts from its saved names.
watch([open, () => props.category], ([isOpen]) => {
  if (isOpen) {
    save.dismissConflict();
    overlay.discard();
  }
});

function close(): void {
  overlay.discard();
  open.value = false;
}

const submit = form.handleSubmit(async (values) => {
  const patch = overlay.patch(values);
  if (isEmptyPatch(patch)) {
    open.value = false;
    return;
  }
  try {
    overlay.load(await save.mutateAsync(patch as CategoryPatch));
    toast({ tone: "success", title: t("configuration.saved") });
    open.value = false;
  } catch {
    // Conflicts keep the draft; other errors are toasted globally.
  }
});
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="title"
    :description="t('taxonomy.edit.description')"
    :persistent="save.isPending.value"
  >
    <AsyncState
      :loading="query.isPending.value"
      :error="query.error.value"
      @retry="query.refetch()"
    >
      <form id="category-edit" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <TemplateMissingAlert :missing="query.data.value?.template_missing" />
        <RevisionConflictAlert
          v-if="save.hasConflict.value"
          :reloading="query.isFetching.value"
          @reload="save.reload()"
        />
        <div v-for="language in languages" :key="language" class="flex items-start gap-2">
          <div class="min-w-0 flex-1">
            <FormField
              v-slot="{ field }"
              :name="`name.${language}`"
              :label="t('taxonomy.edit.nameIn', { language: t(`common.languages.${language}`) })"
              required
            >
              <Input v-bind="field" :lang="language" autocomplete="off" />
            </FormField>
          </div>
          <FieldReset
            v-bind="overlay.resetBinding(`name.${language}`)"
            @reset="overlay.reset(`name.${language}`)"
          />
        </div>
      </form>
    </AsyncState>
    <template #footer>
      <Button :disabled="save.isPending.value" @click="close">
        {{ t("common.actions.cancel") }}
      </Button>
      <Button
        type="submit"
        form="category-edit"
        variant="primary"
        :loading="save.isPending.value"
        :disabled="!query.data.value"
      >
        {{ t("common.actions.save") }}
      </Button>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import {
  FieldReset,
  OverlayFormActions,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayForm,
  type AppConfigurationPatch,
} from "@/features/configuration";
import { isApiError } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { Alert, AsyncState, Card, RevisionConflictAlert } from "@/shared/ui";
import CategoryPicker from "./CategoryPicker.vue";
import { useAllCategories } from "./queries";
import TagEditor from "./TagEditor.vue";

/**
 * Categories (public grouping) and tags (private search keywords) of an
 * application. New category names stay pending until saved; the server then
 * reuses a category with that name or creates it.
 */
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const { t } = useI18n();
const PATHS = ["categories", "tags"] as const;
const schema = z.object({ categories: z.array(z.string()), tags: z.array(z.string()) });

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const categories = useAllCategories();
const overlay = useOverlayForm({
  configuration: configuration.data,
  paths: PATHS,
  schema,
  guard: false,
});
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
  { handledCodes: ["CATEGORY_AMBIGUOUS"] },
);
const { form } = overlay;
const [selected] = form.defineField("categories");
const [tags] = form.defineField("tags");
const pending = ref<string[]>([]);
const failure = ref("");
const tagEditor = ref<InstanceType<typeof TagEditor>>();
const dirty = computed(
  () => overlay.dirty.value || pending.value.length > 0 || !!tagEditor.value?.hasText(),
);
useDirtyGuard(dirty);

// A new baseline (another application, a save, a reload) drops typed names.
watch(
  () => configuration.data.value,
  () => {
    if (!overlay.dirty.value) pending.value = [];
  },
);

function reset(path: string): void {
  if (path === "categories") pending.value = [];
  overlay.reset(path);
}

function discard(): void {
  pending.value = [];
  failure.value = "";
  overlay.discard();
}

const submit = form.handleSubmit(async (values) => {
  if (tagEditor.value && !tagEditor.value.commit()) return;
  const patch = overlay.patch({ ...values, tags: form.values.tags });
  const body: AppConfigurationPatch = patch as AppConfigurationPatch;
  if (pending.value.length > 0) {
    // `new_categories` requires `set.categories` (the server appends to it).
    body.set = { ...body.set, categories: values.categories };
    body.unset = (body.unset ?? []).filter((path) => path !== "categories");
    body.new_categories = [...pending.value];
  }
  if (!Object.keys(body.set ?? {}).length && !body.unset?.length) {
    toast({ title: t("configuration.nothingToSave") });
    return;
  }
  failure.value = "";
  try {
    overlay.load(await save.mutateAsync(body));
    pending.value = [];
    toast({ tone: "success", title: t("configuration.saved") });
  } catch (error) {
    if (isApiError(error, "CATEGORY_AMBIGUOUS")) failure.value = t("taxonomy.categories.ambiguous");
  }
});
</script>

<template>
  <Card :title="t('taxonomy.title')" :description="t('taxonomy.description')">
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form class="flex flex-col gap-5" novalidate @submit.prevent="submit">
        <RevisionConflictAlert
          v-if="save.hasConflict.value"
          :reloading="configuration.isFetching.value"
          @reload="save.reload()"
        />
        <Alert v-if="failure" tone="danger">{{ failure }}</Alert>
        <Alert v-if="categories.error.value" tone="warning">
          {{ t("taxonomy.categories.loadFailed") }}
        </Alert>
        <fieldset class="flex flex-col gap-2" :disabled="readOnly">
          <div class="flex items-center justify-between gap-2">
            <legend class="text-sm font-medium">{{ t("taxonomy.categories.label") }}</legend>
            <FieldReset
              v-bind="overlay.resetBinding('categories')"
              :modified="overlay.modified('categories') || pending.length > 0"
              :disabled="readOnly"
              @reset="reset('categories')"
            />
          </div>
          <CategoryPicker
            v-model="selected"
            v-model:pending="pending"
            :categories="categories.data.value ?? []"
            :label="t('taxonomy.categories.label')"
            :disabled="readOnly"
          />
        </fieldset>
        <fieldset class="flex flex-col gap-2" :disabled="readOnly">
          <div class="flex items-center justify-between gap-2">
            <legend class="text-sm font-medium">{{ t("taxonomy.tags.label") }}</legend>
            <FieldReset
              v-bind="overlay.resetBinding('tags')"
              :disabled="readOnly"
              @reset="reset('tags')"
            />
          </div>
          <TagEditor
            ref="tagEditor"
            v-model="tags"
            :label="t('taxonomy.tags.label')"
            :disabled="readOnly"
          />
        </fieldset>
        <OverlayFormActions
          v-if="!readOnly"
          :dirty="dirty"
          :saving="save.isPending.value"
          @discard="discard"
        />
      </form>
    </AsyncState>
  </Card>
</template>

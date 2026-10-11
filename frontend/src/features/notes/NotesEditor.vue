<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Pencil } from "@lucide/vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { codePointLength, formError, FormField, useDirtyGuard, zodSchema } from "@/shared/forms";
import { useFormat } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Card, RevisionConflictAlert, Textarea } from "@/shared/ui";
import { useNotes, useNotesSave, type NotesOwner } from "./queries";

/**
 * Private administrator notes of a vendor or application (Markdown, at most
 * 12000 characters, never public). They show as the server-rendered HTML;
 * the edit button opens the text. Read-only for deleted items.
 */
const props = defineProps<{ owner: NotesOwner; readOnly?: boolean }>();
const { t } = useI18n();
const format = useFormat();
const MAX = 12000;
// eslint-disable-next-line no-control-regex -- the spec forbids control characters
const CONTROL = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/;

const notes = useNotes(() => props.owner);
const save = useNotesSave(() => props.owner, notes.data);
const editing = ref(false);
const schema = z.object({
  text: z
    .string()
    .refine((value) => codePointLength(value) <= MAX, formError("notes.tooLong", { max: MAX }))
    .refine((value) => !CONTROL.test(value), formError("notes.control")),
});
const { handleSubmit, meta, resetForm, values } = useForm({
  validationSchema: zodSchema(schema),
  initialValues: { text: "" },
  // The field unmounts while the notes are shown; keep the draft meanwhile.
  keepValuesOnUnmount: true,
});
useDirtyGuard(() => meta.value.dirty);

// Follow the server while the draft is unchanged; keep the draft otherwise.
watch(
  () => notes.data.value,
  (data) => {
    if (data && !meta.value.dirty) resetForm({ values: { text: data.text } });
  },
  { immediate: true },
);

const count = computed(() => codePointLength(values.text));

const submit = handleSubmit(async ({ text }) => {
  try {
    const saved = await save.mutateAsync(text);
    resetForm({ values: { text: saved.text } });
    editing.value = false;
    toast({ tone: "success", title: t("notes.saved") });
  } catch {
    // Conflicts keep the draft (alert); other errors are toasted globally.
  }
});

function cancel(): void {
  resetForm({ values: { text: notes.data.value?.text ?? "" } });
  editing.value = false;
}
</script>

<template>
  <Card :title="t('notes.title')" :description="t('notes.description')">
    <template v-if="!readOnly && !editing && notes.data.value" #actions>
      <Button @click="editing = true"><Pencil aria-hidden="true" /> {{ t("notes.edit") }}</Button>
    </template>
    <AsyncState
      :loading="notes.isPending.value"
      :error="notes.error.value"
      @retry="notes.refetch()"
    >
      <form v-if="editing" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <RevisionConflictAlert
          v-if="save.hasConflict.value"
          :reloading="notes.isFetching.value"
          @reload="save.reload()"
        />
        <FormField
          v-slot="{ field }"
          name="text"
          :label="t('notes.label')"
          :description="t('notes.count', { count: format.number(count), max: format.number(MAX) })"
        >
          <Textarea v-bind="field" :rows="14" />
        </FormField>
        <div class="flex flex-wrap items-center justify-end gap-3">
          <p class="me-auto text-sm text-muted" aria-live="polite">
            {{ meta.dirty ? t("notes.unsaved") : "" }}
          </p>
          <Button :disabled="save.isPending.value" @click="cancel">
            {{ t("notes.cancel") }}
          </Button>
          <Button type="submit" variant="primary" :loading="save.isPending.value">
            {{ t("notes.save") }}
          </Button>
        </div>
      </form>
      <!-- eslint-disable-next-line vue/no-v-html -- server-rendered without raw HTML (safe mode) -->
      <div v-else-if="notes.data.value?.html" class="markdown" v-html="notes.data.value.html" />
      <p v-else class="text-sm text-muted">{{ t("notes.empty") }}</p>
    </AsyncState>
  </Card>
</template>

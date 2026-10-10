<script setup lang="ts">
import { computed, watch } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { codePointLength, formError, FormField, useDirtyGuard, zodSchema } from "@/shared/forms";
import { useFormat } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Card, RevisionConflictAlert, Textarea } from "@/shared/ui";
import { useNotes, useNotesSave, type NotesOwner } from "./queries";

/**
 * Private administrator notes of a vendor or application (plain text, at most
 * 12000 characters, never public). Read-only for deleted items.
 */
const props = defineProps<{ owner: NotesOwner; readOnly?: boolean }>();
const { t } = useI18n();
const format = useFormat();
const MAX = 12000;
// eslint-disable-next-line no-control-regex -- the spec forbids control characters
const CONTROL = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/;

const notes = useNotes(() => props.owner);
const save = useNotesSave(() => props.owner, notes.data);
const schema = z.object({
  text: z
    .string()
    .refine((value) => codePointLength(value) <= MAX, formError("notes.tooLong", { max: MAX }))
    .refine((value) => !CONTROL.test(value), formError("notes.control")),
});
const { handleSubmit, meta, resetForm, values } = useForm({
  validationSchema: zodSchema(schema),
  initialValues: { text: "" },
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
    toast({ tone: "success", title: t("notes.saved") });
  } catch {
    // Conflicts keep the draft (alert); other errors are toasted globally.
  }
});

function discard(): void {
  resetForm({ values: { text: notes.data.value?.text ?? "" } });
}
</script>

<template>
  <Card :title="t('notes.title')" :description="t('notes.description')">
    <AsyncState
      :loading="notes.isPending.value"
      :error="notes.error.value"
      @retry="notes.refetch()"
    >
      <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
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
          <Textarea v-bind="field" :rows="14" :disabled="readOnly" />
        </FormField>
        <div v-if="!readOnly" class="flex flex-wrap items-center justify-end gap-3">
          <p class="me-auto text-sm text-muted" aria-live="polite">
            {{ meta.dirty ? t("notes.unsaved") : "" }}
          </p>
          <Button :disabled="!meta.dirty || save.isPending.value" @click="discard">
            {{ t("notes.discard") }}
          </Button>
          <Button type="submit" variant="primary" :loading="save.isPending.value">
            {{ t("notes.save") }}
          </Button>
        </div>
      </form>
    </AsyncState>
  </Card>
</template>

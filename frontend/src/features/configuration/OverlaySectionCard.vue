<script setup lang="ts" generic="Values extends Record<string, unknown>, Patch">
import { useI18n } from "vue-i18n";
import type { z } from "zod";
import { toast } from "@/shared/lib";
import { Card, RevisionConflictAlert } from "@/shared/ui";
import OverlayFormActions from "./OverlayFormActions.vue";
import {
  isEmptyPatch,
  useOverlayForm,
  type OverlayConfiguration,
  type SectionSave,
} from "./overlay";

/**
 * One independently saved card of a configuration page: a form over some
 * overlay paths with its own conflict alert and save bar. The form lives in
 * this component, so FormFields in the slot bind to it (vee-validate provides
 * one form per component). Only the card's edited or reset paths are sent; a
 * save updates the shared revision and other cards keep their drafts.
 */
const props = defineProps<{
  title: string;
  description?: string;
  /** The cached configuration (`useAppConfiguration(...).data.value` and friends). */
  configuration: OverlayConfiguration | undefined;
  /** The configuration is being refetched (after a conflict reload). */
  reloading?: boolean;
  paths: readonly string[];
  schema: z.ZodType<Values>;
  /** The card's own PATCH mutation (`useAppConfigurationPatch` and friends). */
  save: SectionSave<Patch>;
  readOnly?: boolean;
}>();
const emit = defineEmits<{
  /** Failed saves the caller shows itself (codes in the mutation's `handledCodes`). */
  error: [error: unknown];
}>();
const { t } = useI18n();

const overlay = useOverlayForm({
  configuration: () => props.configuration,
  paths: () => props.paths,
  schema: props.schema,
});

const submit = overlay.form.handleSubmit(async (values) => {
  const patch = overlay.patch(values);
  if (isEmptyPatch(patch)) {
    toast({ title: t("configuration.nothingToSave") });
    return;
  }
  try {
    // vue-tsc needs the cast (Patch is a free type parameter); the lint rule cannot see that
    // through the SFC generic. The generated patch types are the typed `{ set, unset }`.
    // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion
    overlay.load(await props.save.mutateAsync(patch as Patch));
    toast({ tone: "success", title: t("configuration.saved") });
  } catch (error) {
    // Conflicts keep the draft (alert); other errors are toasted globally.
    emit("error", error);
  }
});

defineSlots<{ default: (props: { section: typeof overlay }) => unknown }>();
</script>

<template>
  <Card :title="title" :description="description">
    <form class="flex flex-col gap-5" novalidate @submit.prevent="submit">
      <RevisionConflictAlert
        v-if="save.hasConflict.value"
        :reloading="reloading"
        @reload="save.reload()"
      />
      <slot :section="overlay" />
      <OverlayFormActions
        v-if="!readOnly"
        :dirty="overlay.dirty.value"
        :saving="save.isPending.value"
        @discard="overlay.discard()"
      />
    </form>
  </Card>
</template>

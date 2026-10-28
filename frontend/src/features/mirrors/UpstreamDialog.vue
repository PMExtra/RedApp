<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { describeError, isApiError, type ErrorDescription } from "@/shared/api";
import { formError, FormField, useDirtyGuard, zodSchema } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { Alert, AsyncState, Button, Dialog, Input, RevisionConflictAlert } from "@/shared/ui";
import { normalizeOrigin, isUpstreamOrigin } from "./origin";
import {
  useCreateUpstream,
  useUpdateUpstream,
  useUpstream,
  type Upstream,
  type UpstreamRequest,
} from "./queries";

/**
 * Adds an upstream (no `upstream` prop) or renames / moves one. The server
 * verifies the address before saving: its verdict is shown on the address
 * field, a validation failure above the form; other errors are toasted globally.
 * Edits are conditional on the upstream's revision: a conflict keeps the draft.
 */
const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ upstream?: string }>();
const emit = defineEmits<{ created: [upstream: Upstream] }>();
const { t } = useI18n();

const editing = computed(() => props.upstream !== undefined && props.upstream !== "");
const query = useUpstream(
  () => props.upstream ?? "",
  () => open.value && editing.value,
);
const create = useCreateUpstream();
const update = useUpdateUpstream(() => props.upstream ?? "", query.data);
const pending = computed(() => create.isPending.value || update.isPending.value);
const failure = ref<ErrorDescription | null>(null);

const schema = z.object({
  name: z
    .string()
    .refine((value) => value.trim() !== "", formError("mirrors.form.nameRequired"))
    .refine((value) => value.trim().length <= 200, formError("mirrors.form.nameTooLong")),
  origin: z.string().refine(isUpstreamOrigin, formError("mirrors.form.originInvalid")),
});
const { handleSubmit, resetForm, meta, setFieldError } = useForm({
  validationSchema: zodSchema(schema),
  initialValues: { name: "", origin: "" },
});
useDirtyGuard(() => open.value && meta.value.dirty);

function load(upstream: UpstreamRequest | undefined): void {
  resetForm({ values: { name: upstream?.name ?? "", origin: upstream?.origin ?? "" } });
}

// Every opening starts from the saved upstream (or an empty form).
watch(
  open,
  (isOpen) => {
    if (!isOpen) return;
    failure.value = null;
    update.dismissConflict();
    load(editing.value ? query.data.value : undefined);
  },
  { immediate: true },
);
// The upstream arrives after opening; a refetch replaces an untouched form.
watch(query.data, (upstream) => {
  if (open.value && editing.value && !meta.value.dirty) load(upstream);
});

const title = computed(() =>
  editing.value
    ? t("mirrors.form.editTitle", { name: query.data.value?.name ?? "" })
    : t("mirrors.form.addTitle"),
);

const submit = handleSubmit(async (values) => {
  const body: UpstreamRequest = {
    name: values.name.trim(),
    origin: normalizeOrigin(values.origin),
  };
  failure.value = null;
  try {
    if (editing.value) {
      const saved = await update.mutateAsync(body);
      load(saved);
      toast({ tone: "success", title: t("mirrors.form.saved") });
    } else {
      const created = await create.mutateAsync(body);
      load(undefined);
      toast({ tone: "success", title: t("mirrors.form.added", { name: created.name }) });
      emit("created", created);
    }
    open.value = false;
  } catch (error) {
    // Conflicts show the conflict alert (the draft is kept); codes outside
    // UPSTREAM_FORM_CODES are toasted globally.
    if (isApiError(error, "ALREADY_EXISTS")) {
      setFieldError("origin", formError("mirrors.form.exists"));
    } else if (
      isApiError(error, "UPSTREAM_UNAVAILABLE") ||
      isApiError(error, "UPSTREAM_INCOMPATIBLE") ||
      isApiError(error, "LOOP_DETECTED")
    ) {
      setFieldError("origin", formError(`errors.codes.${error.code}`));
    } else if (isApiError(error, "VALIDATION_FAILED")) {
      failure.value = describeError(error);
    }
  }
});
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="title"
    :description="t('mirrors.form.description')"
    :persistent="pending"
  >
    <AsyncState
      :loading="editing && query.isPending.value"
      :error="editing ? query.error.value : undefined"
      @retry="query.refetch()"
    >
      <form id="upstream-form" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <RevisionConflictAlert
          v-if="update.hasConflict.value"
          :reloading="query.isFetching.value"
          @reload="update.reload()"
        />
        <Alert v-if="failure" tone="danger">
          {{ failure.message }}
          <template v-if="failure.detail"> {{ failure.detail }}</template>
        </Alert>
        <FormField v-slot="{ field }" name="name" :label="t('mirrors.form.name')" required>
          <Input v-bind="field" autocomplete="off" maxlength="200" />
        </FormField>
        <FormField
          v-slot="{ field }"
          name="origin"
          :label="t('mirrors.form.origin')"
          :description="t('mirrors.form.originHint')"
          required
        >
          <Input
            v-bind="field"
            type="url"
            inputmode="url"
            spellcheck="false"
            autocomplete="off"
            maxlength="2048"
            class="font-mono"
          />
        </FormField>
      </form>
    </AsyncState>
    <template #footer>
      <Button :disabled="pending" @click="open = false">
        {{ t("common.actions.cancel") }}
      </Button>
      <Button
        type="submit"
        form="upstream-form"
        variant="primary"
        :loading="pending"
        :disabled="editing && !query.data.value"
      >
        {{ editing ? t("common.actions.save") : t("mirrors.form.addTitle") }}
      </Button>
    </template>
  </Dialog>
</template>

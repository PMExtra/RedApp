<script setup lang="ts">
import { ref, watch } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { describeError, isApiError } from "@/shared/api";
import { FormField, formError, utf8Length, zodSchema } from "@/shared/forms";
import { Alert, Button, Dialog, Input } from "@/shared/ui";
import { useSessionStore } from "./store";

/** Change the administrator password; success signs every session out. */
const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ changed: [] }>();
const { t } = useI18n();
const session = useSessionStore();
const failure = ref<string | null>(null);

const schema = z
  .object({
    current: z.string().min(1, formError("ui.form.required")),
    next: z
      .string()
      .refine(
        (value) => utf8Length(value) >= 12 && utf8Length(value) <= 72,
        formError("session.password.lengthRule"),
      ),
    confirm: z.string(),
  })
  .refine((values) => values.next === values.confirm, {
    path: ["confirm"],
    message: formError("session.password.mismatch"),
  });

const initialValues = { current: "", next: "", confirm: "" };
const { handleSubmit, isSubmitting, setFieldError, resetForm } = useForm({
  validationSchema: zodSchema(schema),
  initialValues,
});

watch(open, (value) => {
  if (!value) {
    resetForm({ values: initialValues });
    failure.value = null;
  }
});

const submit = handleSubmit(async ({ current, next }) => {
  failure.value = null;
  try {
    await session.changePassword(current, next);
    open.value = false;
    emit("changed");
  } catch (error) {
    if (isApiError(error, "CURRENT_PASSWORD_INCORRECT")) {
      setFieldError("current", formError("errors.codes.CURRENT_PASSWORD_INCORRECT"));
    } else if (isApiError(error, "PASSWORD_INVALID")) {
      setFieldError("next", formError("errors.codes.PASSWORD_INVALID"));
    } else {
      failure.value = describeError(error).message;
    }
  }
});
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="t('session.password.title')"
    :description="t('session.password.description')"
    :persistent="isSubmitting"
  >
    <form id="password-change" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <Alert v-if="failure" tone="danger">{{ failure }}</Alert>
      <FormField v-slot="{ field }" name="current" :label="t('session.password.current')" required>
        <Input v-bind="field" type="password" autocomplete="current-password" />
      </FormField>
      <FormField
        v-slot="{ field }"
        name="next"
        :label="t('session.password.new')"
        :description="t('session.password.lengthRule')"
        required
      >
        <Input v-bind="field" type="password" autocomplete="new-password" />
      </FormField>
      <FormField v-slot="{ field }" name="confirm" :label="t('session.password.confirm')" required>
        <Input v-bind="field" type="password" autocomplete="new-password" />
      </FormField>
    </form>
    <template #footer>
      <Button :disabled="isSubmitting" @click="open = false">
        {{ t("common.actions.cancel") }}
      </Button>
      <Button type="submit" form="password-change" variant="primary" :loading="isSubmitting">
        {{ t("session.password.submit") }}
      </Button>
    </template>
  </Dialog>
</template>

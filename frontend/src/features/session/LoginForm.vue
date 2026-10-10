<script setup lang="ts">
import { ref } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { z } from "zod";
import { describeError, isApiError } from "@/shared/api";
import { FormField, formError, zodSchema } from "@/shared/forms";
import { Alert, Button, Input } from "@/shared/ui";
import { useSessionStore } from "./store";

const emit = defineEmits<{ success: [] }>();
const { t } = useI18n();
const session = useSessionStore();
const failure = ref<ReturnType<typeof describeError> | null>(null);

const { handleSubmit, isSubmitting, setFieldError, resetForm } = useForm({
  validationSchema: zodSchema(
    z.object({ password: z.string().min(1, formError("session.login.passwordRequired")) }),
  ),
  initialValues: { password: "" },
});

const submit = handleSubmit(async ({ password }) => {
  failure.value = null;
  try {
    await session.signIn(password);
    resetForm();
    emit("success");
  } catch (error) {
    if (isApiError(error, "LOGIN_FAILED")) {
      setFieldError("password", formError("errors.codes.LOGIN_FAILED"));
    } else {
      failure.value = describeError(error);
    }
  }
});
</script>

<template>
  <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
    <Alert v-if="failure" tone="danger">{{ failure.message }}</Alert>
    <FormField v-slot="{ field }" name="password" :label="t('session.login.password')" required>
      <Input v-bind="field" type="password" autocomplete="current-password" />
    </FormField>
    <Button type="submit" variant="primary" :loading="isSubmitting">
      {{ isSubmitting ? t("session.login.signingIn") : t("session.login.submit") }}
    </Button>
  </form>
</template>

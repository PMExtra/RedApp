<script setup lang="ts">
// Test fixture: a vee-validate form guarded against losing its draft.
import { useForm } from "vee-validate";
import { z } from "zod";
import { FormField, formError, useDirtyGuard, zodSchema } from "@/shared/forms";
import { Input } from "@/shared/ui";

const { meta } = useForm({
  validationSchema: zodSchema(
    z.object({ title: z.string().max(5, formError("ui.form.tooLong", { max: 5 })) }),
  ),
  initialValues: { title: "Saved" },
});
useDirtyGuard(() => meta.value.dirty);
</script>

<template>
  <form>
    <FormField v-slot="{ field }" name="title" label="Title">
      <Input v-bind="field" />
    </FormField>
  </form>
</template>

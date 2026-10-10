<script setup lang="ts">
import { useId } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { z } from "zod";
import { isApiError } from "@/shared/api";
import { formError, FormField, useDirtyGuard, zodSchema } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { Button, Card, Input, Switch } from "@/shared/ui";
import IconField from "./IconField.vue";
import { vendorRoute } from "./links";
import LocalizedTextFields from "./LocalizedTextFields.vue";
import { useCreateVendor } from "./queries";
import { descriptionSchema, iconSchema, nameSchema, vendorIdSchema } from "./schemas";

/** Creates a custom vendor (no template, proxy `inherit`). */
const { t } = useI18n();
const router = useRouter();
const create = useCreateVendor();
const enabledId = useId();

const schema = z.object({
  id: vendorIdSchema,
  name: nameSchema,
  description: descriptionSchema,
  icon: iconSchema,
  localized_icons: z.object({ en: iconSchema, "zh-CN": iconSchema }),
  enabled: z.boolean(),
});
const initialValues = {
  id: "",
  name: { en: "", "zh-CN": "" },
  description: { en: "", "zh-CN": "" },
  icon: "",
  localized_icons: { en: "", "zh-CN": "" },
  enabled: true,
};
const { handleSubmit, meta, defineField, setFieldError, resetForm, isSubmitting } = useForm({
  validationSchema: zodSchema(schema),
  initialValues,
});
useDirtyGuard(() => meta.value.dirty);
const [icon] = defineField("icon");
const [iconEn] = defineField("localized_icons.en");
const [iconZh] = defineField("localized_icons.zh-CN");
const [enabled] = defineField("enabled");

const submit = handleSubmit(async (values) => {
  try {
    const vendor = await create.mutateAsync({ ...values, id: values.id.trim() });
    resetForm({ values });
    toast({ tone: "success", title: t("directory.create.vendorCreated", { id: vendor.id }) });
    await router.replace(vendorRoute(vendor.id));
  } catch (error) {
    if (isApiError(error, "ALREADY_EXISTS")) {
      setFieldError("id", formError("directory.validation.vendorExists"));
    }
  }
});
</script>

<template>
  <form class="flex flex-col gap-6" novalidate @submit.prevent="submit">
    <Card :title="t('directory.sections.details')">
      <div class="flex flex-col gap-5">
        <div class="grid gap-4 md:grid-cols-2">
          <FormField
            v-slot="{ field }"
            name="id"
            :label="t('directory.fields.vendorId')"
            :description="t('directory.fields.idHint')"
            required
          >
            <Input
              v-bind="field"
              autocomplete="off"
              autocapitalize="none"
              spellcheck="false"
              maxlength="63"
              class="font-mono"
            />
          </FormField>
          <div class="flex items-start gap-3 pt-7">
            <Switch :id="enabledId" v-model="enabled" />
            <label :for="enabledId" class="text-sm font-medium">
              {{ t("directory.fields.enabled") }}
            </label>
          </div>
        </div>
        <LocalizedTextFields />
      </div>
    </Card>
    <Card :title="t('directory.sections.logos')" :description="t('directory.icon.vendorHint')">
      <div class="grid gap-4 sm:grid-cols-3">
        <IconField
          v-model="icon"
          :label="t('directory.icon.defaultLogo')"
          :empty-text="t('directory.icon.none')"
          variant="logo"
        />
        <IconField
          v-model="iconEn"
          :label="t('directory.icon.englishLogo')"
          :empty-text="t('directory.icon.usesDefault')"
          variant="logo"
        />
        <IconField
          v-model="iconZh"
          :label="t('directory.icon.chineseLogo')"
          :empty-text="t('directory.icon.usesDefault')"
          variant="logo"
        />
      </div>
    </Card>
    <div class="flex justify-end gap-3">
      <Button type="submit" variant="primary" :loading="isSubmitting">
        {{ t("directory.create.vendorSubmit") }}
      </Button>
    </div>
  </form>
</template>

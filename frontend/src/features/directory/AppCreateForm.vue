<script setup lang="ts">
import { computed, useId, watch } from "vue";
import { useForm } from "vee-validate";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { z } from "zod";
import { isApiError, type Schema } from "@/shared/api";
import { formError, FormField, useDirtyGuard, zodSchema } from "@/shared/forms";
import { useLocalized } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Card, Input, RadioGroup, Switch } from "@/shared/ui";
import IconField from "./IconField.vue";
import { appRoute, isReleaseProvider } from "./links";
import LocalizedTextFields from "./LocalizedTextFields.vue";
import { useCreateApp, useProviders, type Provider, type ProviderKey } from "./queries";
import {
  appIdSchema,
  baseUrlsSchema,
  descriptionSchema,
  httpUrlSchema,
  iconSchema,
  nameSchema,
  strategySchema,
  ttlSchema,
} from "./schemas";
import SourceFields from "./SourceFields.vue";

/**
 * Creates an application under `vendor`. Provider, vendor and ID are fixed
 * afterwards; choosing a provider fills in its default upstream and TTL.
 */
const props = defineProps<{ vendor: string }>();
const { t } = useI18n();
const localized = useLocalized();
const router = useRouter();
const providers = useProviders();
const create = useCreateApp();
const enabledId = useId();

const PROVIDERS = ["info", "hosted", "http-cache", "codex", "claude-code"] as const;
const schema = z
  .object({
    provider: z.enum(PROVIDERS),
    id: appIdSchema,
    name: nameSchema,
    description: descriptionSchema,
    icon: iconSchema,
    enabled: z.boolean(),
    base_url: z.string(),
    base_urls: z.array(z.string()),
    source_strategy: strategySchema,
    cache_ttl_seconds: z.number().nullable(),
  })
  .superRefine((values, context) => {
    // Only the provider's own upstream fields are validated and sent.
    const check = (rule: z.ZodType, value: unknown, path: string) => {
      const result = rule.safeParse(value);
      if (result.success) return;
      for (const issue of result.error.issues) {
        context.addIssue({ code: "custom", path: [path, ...issue.path], message: issue.message });
      }
    };
    if (values.provider === "http-cache") {
      check(baseUrlsSchema, values.base_urls, "base_urls");
      check(ttlSchema, values.cache_ttl_seconds, "cache_ttl_seconds");
    } else if (isReleaseProvider(values.provider)) {
      check(httpUrlSchema, values.base_url, "base_url");
    }
  });

const { handleSubmit, meta, defineField, setFieldError, setValues, resetForm, isSubmitting } =
  useForm({
    validationSchema: zodSchema(schema),
    initialValues: {
      provider: "info" as ProviderKey,
      id: "",
      name: { en: "", "zh-CN": "" },
      description: { en: "", "zh-CN": "" },
      icon: "",
      enabled: true,
      base_url: "",
      base_urls: [""],
      source_strategy: "ordered" as const,
      cache_ttl_seconds: null as number | null,
    },
  });
useDirtyGuard(() => meta.value.dirty);
const [provider] = defineField("provider");
const [icon] = defineField("icon");
const [enabled] = defineField("enabled");

const definitions = computed<Provider[]>(() => providers.data.value?.items ?? []);
const providerOptions = computed(() =>
  definitions.value.map((item) => ({
    value: item.key,
    label: localized(item.name),
    description: localized(item.description),
  })),
);

// Provider defaults: base URL for release providers, TTL for cached ones.
watch(
  [provider, definitions],
  ([key]) => {
    const definition = definitions.value.find((item) => item.key === key);
    setValues(
      {
        base_url: definition?.default_base_url ?? "",
        base_urls: [definition?.default_base_url ?? ""],
        source_strategy: "ordered",
        cache_ttl_seconds: definition?.default_cache_ttl_seconds ?? null,
      },
      false,
    );
  },
  { immediate: true },
);

const submit = handleSubmit(async (values) => {
  const upstream: Partial<Schema<"AppCreate">> =
    values.provider === "http-cache"
      ? {
          base_urls: values.base_urls.map((url) => url.trim()),
          source_strategy: values.source_strategy,
          cache_ttl_seconds: values.cache_ttl_seconds ?? 0,
        }
      : isReleaseProvider(values.provider)
        ? { base_url: values.base_url.trim() }
        : {};
  try {
    const app = await create.mutateAsync({
      vendor: props.vendor,
      id: values.id.trim(),
      provider: values.provider,
      name: values.name,
      description: values.description,
      icon: values.icon,
      enabled: values.enabled,
      ...upstream,
    });
    resetForm({ values });
    toast({ tone: "success", title: t("directory.create.appCreated", { key: app.key }) });
    await router.replace(appRoute(app, "settings"));
  } catch (error) {
    if (isApiError(error, "ALREADY_EXISTS")) {
      setFieldError("id", formError("directory.validation.appExists"));
    }
  }
});
</script>

<template>
  <AsyncState
    :loading="providers.isPending.value"
    :error="providers.error.value"
    @retry="providers.refetch()"
  >
    <form class="flex flex-col gap-6" novalidate @submit.prevent="submit">
      <Card
        :title="t('directory.fields.provider')"
        :description="t('directory.create.providerHint')"
      >
        <FormField
          v-slot="{ field }"
          name="provider"
          :label="t('directory.fields.provider')"
          hide-label
        >
          <RadioGroup
            v-bind="field"
            :options="providerOptions"
            :aria-label="t('directory.fields.provider')"
          />
        </FormField>
      </Card>

      <Card :title="t('directory.sections.details')">
        <div class="flex flex-col gap-5">
          <div class="grid gap-4 md:grid-cols-2">
            <FormField
              v-slot="{ field }"
              name="id"
              :label="t('directory.fields.appId')"
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
          <div class="max-w-xs">
            <IconField
              v-model="icon"
              :label="t('directory.icon.appIcon')"
              :empty-text="t('directory.icon.none')"
            />
          </div>
        </div>
      </Card>

      <Card
        v-if="provider === 'http-cache' || isReleaseProvider(provider)"
        :title="t('directory.sections.source')"
      >
        <SourceFields :provider="provider" />
      </Card>

      <div class="flex justify-end gap-3">
        <Button type="submit" variant="primary" :loading="isSubmitting">
          {{ t("directory.create.appSubmit") }}
        </Button>
      </div>
    </form>
  </AsyncState>
</template>

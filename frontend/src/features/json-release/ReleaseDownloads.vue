<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { Download } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { api, queryKey, unwrap } from "@/shared/api";
import { Alert, AsyncState, Card, Field, Select } from "@/shared/ui";
import { detectEnvironment, recommend, type Artifact, type Environment } from "./recommendation";
const props = defineProps<{ vendor: string; app: string }>();
const { t } = useI18n();
const query = useQuery({
  queryKey: computed(() =>
    queryKey("getReleaseDownloads", { vendor: props.vendor, app: props.app }),
  ),
  queryFn: ({ signal }) =>
    unwrap(
      api.GET("/api/apps/{vendor}/{app}/downloads", {
        params: { path: { vendor: props.vendor, app: props.app } },
        signal,
      }),
    ),
});
const environment = ref<Environment>({});
const selected = ref<string>();
const manual = ref(false);
const artifacts = computed(() => query.data.value?.artifacts ?? []);
const label = (a: Artifact) =>
  [t(`releaseDownloads.os.${a.os}`), a.arch, a.format, a.scope, a.variant]
    .filter(Boolean)
    .join(" · ");
const options = computed(() => artifacts.value.map((a) => ({ value: a.key, label: label(a) })));
const chosen = computed(() => artifacts.value.find((a) => a.key === selected.value));
watch(
  [artifacts, environment],
  () => {
    if (manual.value) {
      if (!artifacts.value.some((a) => a.key === selected.value)) selected.value = undefined;
    } else selected.value = recommend(artifacts.value, environment.value);
  },
  { immediate: true },
);
function choose(key: string | undefined) {
  manual.value = true;
  selected.value = key;
}
onMounted(async () => {
  environment.value = detectEnvironment(navigator.userAgent);
  const browser = navigator as Navigator & {
    userAgentData?: {
      getHighEntropyValues: (
        keys: string[],
      ) => Promise<{ platform?: string; architecture?: string; bitness?: string }>;
    };
  };
  try {
    const hints = await browser.userAgentData?.getHighEntropyValues([
      "platform",
      "architecture",
      "bitness",
    ]);
    if (hints) environment.value = detectEnvironment(navigator.userAgent, hints);
  } catch {
    /* Manual selection remains available. */
  }
});
</script>
<template>
  <Card :title="t('releaseDownloads.title')">
    <AsyncState
      :loading="query.isPending.value"
      :error="query.error.value"
      @retry="query.refetch()"
    >
      <div v-if="query.data.value" class="flex flex-col gap-4">
        <Alert v-if="query.data.value.stale" tone="warning">{{
          t("releaseDownloads.stale")
        }}</Alert>
        <p>{{ t("releaseDownloads.version", { version: query.data.value.version }) }}</p>
        <Field :label="t('releaseDownloads.choice')" :description="t('releaseDownloads.hint')"
          ><template #default="{ control }"
            ><Select
              v-bind="control"
              :model-value="selected"
              :options="options"
              :placeholder="t('releaseDownloads.choose')"
              @update:model-value="choose" /></template
        ></Field>
        <a
          v-if="chosen"
          :href="chosen.url"
          class="inline-flex w-fit items-center gap-2 rounded-lg bg-primary px-4 py-2 text-primary-fg hover:bg-primary-hover"
          ><Download class="size-4" aria-hidden="true" />{{
            t("releaseDownloads.download", { package: label(chosen) })
          }}</a
        >
        <p v-if="artifacts.length === 0" class="text-muted">
          {{ t("releaseDownloads.unavailable") }}
        </p>
      </div>
    </AsyncState>
  </Card>
</template>

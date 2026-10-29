<script setup lang="ts">
import { computed, ref } from "vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { useAppConfiguration } from "@/features/configuration";
import { api, queryKey, unwrap } from "@/shared/api";
import { toast } from "@/shared/lib";
import { Button, Card, Field, Input } from "@/shared/ui";

/**
 * Discovers one version of a JSON release with its configuration's lookup,
 * for the versions tab. Without a lookup it explains how to add one.
 */
const props = defineProps<{ vendor: string; app: string }>();
const { t } = useI18n();
const client = useQueryClient();
const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const available = computed(() => !!configuration.data.value?.effective.json_release?.lookup);
const version = ref("");
const lookup = useMutation({
  mutationFn: () =>
    unwrap(
      api.POST("/admin/api/apps/{vendor}/{app}/releases/lookup", {
        params: { path: { vendor: props.vendor, app: props.app } },
        body: { version: version.value.trim() },
      }),
    ),
  onSuccess: async (release) => {
    version.value = "";
    toast({ tone: "success", title: t("releaseLookup.found", { version: release.version }) });
    await Promise.all(
      ["listVersions", "listReleases", "getReleaseDownloads"].map((operation) =>
        client.invalidateQueries({ queryKey: queryKey(operation as "listVersions") }),
      ),
    );
  },
});
</script>

<template>
  <Card :title="t('releaseLookup.title')" :description="t('releaseLookup.description')">
    <p v-if="configuration.data.value && !available" class="text-sm text-muted">
      {{ t("releaseLookup.unavailable") }}
    </p>
    <form
      v-else-if="available"
      class="flex flex-wrap items-end gap-3"
      novalidate
      @submit.prevent="lookup.mutate()"
    >
      <Field :label="t('releaseLookup.version')">
        <template #default="{ control }">
          <Input v-bind="control" v-model="version" placeholder="1.2.3" class="w-48" />
        </template>
      </Field>
      <Button
        type="submit"
        variant="primary"
        :loading="lookup.isPending.value"
        :disabled="!version.trim()"
      >
        {{ t("releaseLookup.submit") }}
      </Button>
    </form>
  </Card>
</template>

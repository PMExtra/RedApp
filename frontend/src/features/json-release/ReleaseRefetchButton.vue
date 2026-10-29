<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { api, queryKey, unwrap } from "@/shared/api";
import { confirm, toast } from "@/shared/lib";
import { Button } from "@/shared/ui";

/**
 * Re-fetches one version of a JSON release: for an upstream that published
 * another build of a version RedApp already knows. Its cached files and
 * metadata are replaced by what the upstream publishes now.
 */
const props = defineProps<{ vendor: string; app: string; version: string }>();
const { t } = useI18n();
const client = useQueryClient();
const refetch = useMutation({
  mutationFn: () =>
    unwrap(
      api.POST("/admin/api/apps/{vendor}/{app}/versions/{version}/refetch", {
        params: { path: { vendor: props.vendor, app: props.app, version: props.version } },
      }),
    ),
  onSuccess: (result) =>
    toast({
      tone: "success",
      title: t(result.replaced ? "releaseRefetch.done" : "releaseRefetch.unchanged", {
        version: props.version,
      }),
    }),
  onSettled: async () => {
    await Promise.all(
      ["listVersions", "listResources", "listReleases", "getReleaseDownloads"].map((operation) =>
        client.invalidateQueries({ queryKey: queryKey(operation as "listVersions") }),
      ),
    );
  },
});

async function run() {
  const confirmed = await confirm({
    title: t("releaseRefetch.title", { version: props.version }),
    description: t("releaseRefetch.description"),
    confirmLabel: t("releaseRefetch.confirm"),
    tone: "danger",
  });
  if (confirmed) refetch.mutate();
}
</script>

<template>
  <Button size="sm" :loading="refetch.isPending.value" @click="run">
    <RefreshCw aria-hidden="true" />
    {{ t("releaseRefetch.action") }}
  </Button>
</template>

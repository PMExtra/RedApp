<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Download } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { distributionPath } from "@/shared/lib";
import { AsyncState, Card, Pagination, RelativeTime } from "@/shared/ui";
import { HOSTED_PAGE_SIZE, usePublicHostedFiles } from "./queries";

/** Download list of a hosted application, 25 files per page. */
const props = defineProps<{ app: Schema<"PublicApp"> }>();
const { t } = useI18n();
const format = useFormat();
const page = ref(1);
watch(
  () => props.app.key,
  () => {
    page.value = 1;
  },
);
const files = usePublicHostedFiles(() => props.app, page);
const data = computed(() => files.data.value);
function href(path: string) {
  const [vendor = "", app = ""] = props.app.key.split("/");
  return distributionPath(vendor, app, path);
}
</script>

<template>
  <Card
    :title="t('catalog.hosted.title')"
    :description="data && data.total > 0 ? t('catalog.hosted.count', data.total) : undefined"
  >
    <AsyncState
      :loading="files.isPending.value"
      :error="files.error.value"
      :empty="data?.items.length === 0"
      :empty-text="data?.total ? t('catalog.hosted.pageEmpty') : t('catalog.hosted.empty')"
      @retry="files.refetch()"
    >
      <ul class="-my-2 divide-y divide-border" :aria-busy="files.isPlaceholderData.value">
        <li
          v-for="file in data?.items"
          :key="file.id"
          class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2"
        >
          <a
            :href="href(file.path)"
            download
            class="flex min-w-0 items-center gap-2 rounded-sm font-mono text-sm break-all text-primary hover:underline focus-ring"
          >
            <Download class="size-4 shrink-0" aria-hidden="true" />
            {{ file.path }}
          </a>
          <span class="flex shrink-0 items-center gap-3 text-xs text-muted tabular-nums">
            <span>{{ format.bytes(file.size_bytes) }}</span>
            <RelativeTime :value="file.created_at" />
          </span>
        </li>
      </ul>
    </AsyncState>
    <template v-if="data && data.total_pages > 1" #footer>
      <Pagination v-model:page="page" :total="data.total" :page-size="HOSTED_PAGE_SIZE" />
    </template>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Download, Pencil, Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { isApiError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { confirm, distributionPath, toast } from "@/shared/lib";
import {
  Card,
  DataTable,
  IconButton,
  Pagination,
  RelativeTime,
  type DataTableColumn,
} from "@/shared/ui";
import { useDeleteHostedFile, useHostedFiles, type HostedFile } from "./queries";

/** The hosted files, page by page: download, delete, or pick one to replace. */
const props = defineProps<{
  vendor: string;
  app: string;
  /** Deleted applications keep their files readable and deletable only. */
  deleted?: boolean;
  /** A transfer is running: no replacing or deleting meanwhile. */
  busy?: boolean;
}>();
const emit = defineEmits<{ replace: [file: HostedFile] }>();
const { t } = useI18n();
const format = useFormat();

const page = ref(1);
const files = useHostedFiles(
  () => props.vendor,
  () => props.app,
  page,
);
// Deleting the last files of the last page empties it: move to the new last page.
watch(
  () => files.data.value?.total_pages,
  (pages) => {
    const last = Math.max(1, pages ?? 1);
    if (pages !== undefined && page.value > last) page.value = last;
  },
);
const remove = useDeleteHostedFile(
  () => props.vendor,
  () => props.app,
);
const columns = computed<DataTableColumn[]>(() => [
  { key: "path", label: t("hosted.list.path") },
  { key: "size_bytes", label: t("hosted.list.size"), align: "end" },
  { key: "sha256", label: t("hosted.list.sha256") },
  { key: "created_at", label: t("hosted.list.created") },
  { key: "actions", label: t("hosted.list.actions"), hideLabel: true, align: "end" },
]);

async function deleteFile(file: HostedFile) {
  const confirmed = await confirm({
    title: t("hosted.delete.title"),
    description: t("hosted.delete.description", { path: file.path }),
    confirmLabel: t("common.actions.delete"),
    tone: "danger",
  });
  if (!confirmed) return;
  remove.mutate(file.id, {
    onSuccess: () =>
      toast({ tone: "success", title: t("hosted.delete.done", { path: file.path }) }),
    onError: (error) => {
      if (isApiError(error, "FILE_NOT_FOUND")) {
        toast({ tone: "info", title: t("hosted.delete.gone", { path: file.path }) });
      }
    },
  });
}
</script>

<template>
  <Card :title="t('hosted.list.title')" :description="t('hosted.list.description')">
    <div class="flex flex-col gap-3">
      <DataTable
        :columns="columns"
        :rows="files.data.value?.items"
        :row-key="(row) => row.id"
        :loading="files.isPending.value"
        :error="files.error.value"
        :caption="t('hosted.list.title')"
        :empty-text="t('hosted.list.empty')"
        @retry="files.refetch()"
      >
        <template #cell-path="{ row }">
          <a
            :href="distributionPath(vendor, app, row.path)"
            download
            class="inline-flex items-center gap-1 font-mono text-xs break-all text-primary hover:underline focus-ring"
          >
            <Download class="size-3.5 shrink-0" aria-hidden="true" />
            {{ row.path }}
          </a>
        </template>
        <template #cell-size_bytes="{ row }">
          <span class="tabular-nums">{{ format.bytes(row.size_bytes) }}</span>
        </template>
        <template #cell-sha256="{ row }">
          <span class="font-mono text-xs" :title="row.sha256">{{ row.sha256.slice(0, 12) }}…</span>
        </template>
        <template #cell-created_at="{ row }">
          <RelativeTime :value="row.created_at" />
        </template>
        <template #cell-actions="{ row }">
          <div class="flex justify-end gap-1">
            <IconButton
              v-if="!deleted"
              size="sm"
              :label="t('hosted.list.replace', { path: row.path })"
              :disabled="busy"
              @click="emit('replace', row)"
            >
              <Pencil aria-hidden="true" />
            </IconButton>
            <IconButton
              size="sm"
              :label="t('hosted.list.delete', { path: row.path })"
              :disabled="busy || remove.isPending.value"
              @click="deleteFile(row)"
            >
              <Trash2 aria-hidden="true" />
            </IconButton>
          </div>
        </template>
      </DataTable>
      <Pagination
        v-if="(files.data.value?.total_pages ?? 1) > 1"
        v-model:page="page"
        :total="files.data.value?.total ?? 0"
        :page-size="files.data.value?.limit ?? 25"
      />
    </div>
  </Card>
</template>

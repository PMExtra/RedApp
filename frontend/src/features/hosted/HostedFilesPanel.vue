<script setup lang="ts">
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { Alert } from "@/shared/ui";
import HostedFileForm from "./HostedFileForm.vue";
import HostedFileList from "./HostedFileList.vue";
import type { HostedFile } from "./queries";

/**
 * Files of a hosted application: upload with progress or import once from a
 * URL (create, or replace a specific file version), list, download, delete.
 */
defineProps<{
  vendor: string;
  app: string;
  /** Deleted applications keep their files readable and deletable only. */
  deleted?: boolean;
}>();
const { t } = useI18n();
const replacing = ref<HostedFile | null>(null);
const busy = ref(false);
</script>

<template>
  <div class="flex flex-col gap-6">
    <Alert v-if="deleted" tone="warning">{{ t("hosted.deleted") }}</Alert>
    <HostedFileForm
      v-else
      v-model:replacing="replacing"
      :vendor="vendor"
      :app="app"
      @update:busy="busy = $event"
    />
    <HostedFileList
      :vendor="vendor"
      :app="app"
      :deleted="deleted"
      :busy="busy"
      @replace="replacing = $event"
    />
  </div>
</template>

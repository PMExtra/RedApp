<script setup lang="ts">
import { onUnmounted, ref, watch } from "vue";
import { bytes } from "../api";
import type { NumberedPage } from "../composables/useNumberedCollection";
import { downloadPath, type HostedFile } from "../hosted";
import { errorText, t } from "../i18n";
import PageNavigation from "./PageNavigation.vue";
const props = defineProps<{ application: string }>();
const rows = ref<HostedFile[]>([]),
  page = ref(1),
  total = ref(0),
  pages = ref(1),
  loading = ref(false),
  error = ref<unknown>();
let controller: AbortController | undefined,
  ticket = 0;
async function load() {
  controller?.abort();
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  loading.value = true;
  try {
    const result = await fetch(
      `/api/apps/${props.application}/files?page=${page.value}&limit=25`,
      { signal: request.signal, credentials: "omit" },
    );
    if (!result.ok) throw Error("Files unavailable");
    const data = (await result.json()) as NumberedPage<HostedFile>;
    if (attempt === ticket) {
      rows.value = data.items;
      page.value = data.page;
      pages.value = data.total_pages;
      total.value = data.total;
      error.value = undefined;
    }
  } catch (reason) {
    if (attempt === ticket) error.value = reason;
  } finally {
    if (attempt === ticket) loading.value = false;
  }
}
function go(next: number) {
  if (next >= 1 && next <= pages.value) {
    page.value = next;
    void load();
  }
}
watch(
  () => props.application,
  () => {
    rows.value = [];
    page.value = 1;
    total.value = 0;
    pages.value = 1;
    void load();
  },
  { immediate: true, flush: "sync" },
);
onUnmounted(() => {
  ticket++;
  controller?.abort();
});
</script>
<template>
  <section class="panel hosted-downloads">
    <h2>{{ t("Download files") }}</h2>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <ul class="download-list">
      <li v-for="file in rows" :key="file.id">
        <a :href="downloadPath(application, file.path)" download>{{
          file.path
        }}</a
        ><span class="muted">{{ bytes(file.size_bytes) }}</span>
      </li>
    </ul>
    <p v-if="!loading && !error && !rows.length">
      {{ t("No files saved yet.") }}
    </p>
    <PageNavigation
      :label="t('File pages')"
      :page="page"
      :total="total"
      :total-pages="pages"
      :previous="page > 1"
      :next="page < pages"
      :loading="loading"
      @previous="go(page - 1)"
      @next="go(page + 1)"
      @go="go"
      @refresh="load"
    />
  </section>
</template>

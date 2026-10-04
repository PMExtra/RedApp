<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { appAPI } from "../bootstrap";
import { bytes } from "../api";
import { errorText, t } from "../i18n";
import { usePagedCollection } from "../composables/usePagedCollection";
import { maintenanceStatus, type PreviewItem } from "../maintenanceJobs";
import PageNavigation from "./PageNavigation.vue";
const props = withDefaults(defineProps<{ application: string; kind: "cleanup" | "refresh"; jobId: string; sourceQuery?: string; refreshToken?: string }>(), { sourceQuery: "", refreshToken: "" });
const page = reactive(usePagedCollection<PreviewItem>(computed(() => `${appAPI(props.application)}/cache/${props.kind}/${encodeURIComponent(props.jobId)}/items${props.sourceQuery}`), ref(false), 25));
watch(() => props.refreshToken, () => page.reload());
</script>
<template>
  <section class="preview-files" :aria-label="t('Selected files')">
    <h4>{{ t('Selected files') }}</h4>
    <p class="muted small-text">{{ t('This page shows up to 25 files. Execution applies to the entire frozen selection, not only this page.') }}</p>
    <p v-if="page.error" class="error" role="alert">{{ errorText(page.error) }}</p>
    <p v-if="page.loading && !page.loaded" role="status">{{ t('Loading…') }}</p>
    <div v-if="page.loaded && page.items.length" class="table-wrap" tabindex="0"><table><thead><tr><th>{{ t('File') }}</th><th>{{ t('Size') }}</th><th>{{ t('Result') }}</th></tr></thead><tbody><tr v-for="item in page.items" :key="item.ordinal"><td><code>{{ item.path }}</code><small>{{ item.generation_id }}</small></td><td>{{ bytes(item.size_bytes) }}</td><td>{{ maintenanceStatus(item.result_status) }}<small v-if="item.error_code">{{ item.error_code }}</small></td></tr></tbody></table></div>
    <p v-else-if="page.loaded && !page.error" class="muted">{{ t('No files on this page.') }}</p>
    <PageNavigation :label="t('Preview file pages')" :page="page.page" :previous="page.previousAvailable" :next="page.nextAvailable" :loading="page.loading" @previous="page.previous" @next="page.next" @refresh="page.refresh" />
  </section>
</template>

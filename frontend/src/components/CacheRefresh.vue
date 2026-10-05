<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { computed, onUnmounted, ref, watch } from "vue";
import { api, bytes } from "../api";
import { appAPI } from "../bootstrap";
import type { MatcherSpec } from "../cachePolicy";
import { maintenanceStatus, type MaintenancePreview, type RefreshItem, type RefreshSummary } from "../maintenanceJobs";
import { usePageStatus } from "../composables/usePageStatus";
import { errorText, localDate, t } from "../i18n";
import MatcherInput from "./MatcherInput.vue";
import PreviewFiles from "./PreviewFiles.vue";
const props = defineProps<{ application: string }>();
const emit = defineEmits<{ changed: [] }>();
const match = ref<MatcherSpec>({ type: "glob", pattern: "/" });
const job = ref<MaintenancePreview>(), phase = ref<"preview" | "execute" | "single">();
const error = ref<unknown>(), single = ref<RefreshItem>();
const running = computed(() => job.value?.state === "running" || job.value?.state === "building");
const busy = computed(() => !!phase.value || running.value);
const summary = computed(() => {
  const receipt = job.value?.result;
  return receipt && typeof receipt === 'object' && 'refreshed' in receipt ? receipt as RefreshSummary : undefined;
});
const jobPath = computed(() => job.value ? `${appAPI(props.application)}/cache/refresh/${encodeURIComponent(job.value.id)}` : "");
const { status: polled, error: pollError, loading: polling, refresh: poll } = usePageStatus<{ job: MaintenancePreview }>(jobPath, running);
let ticket = 0, controller: AbortController | undefined;
function invalidate() {
  ticket++; controller?.abort(); controller = undefined;
  job.value = undefined; phase.value = undefined; error.value = undefined; single.value = undefined;
}
watch(match, invalidate, { deep: true, flush: "sync" });
watch(() => props.application, () => { invalidate(); match.value = { type: 'glob', pattern: '/' }; }, { flush: 'sync' });
watch(polled, (value) => {
  if (!value || value.job.id !== job.value?.id) return;
  const wasRunning = running.value;
  job.value = value.job;
  if (wasRunning && !running.value) emit('changed');
});
async function plan() {
  if (busy.value || !match.value.pattern) return;
  const request = new AbortController(), attempt = ++ticket;
  controller = request; phase.value = 'preview'; error.value = undefined; job.value = undefined; single.value = undefined;
  try {
    const response = await api<{ job: MaintenancePreview }>(`${appAPI(props.application)}/cache/refresh/preview`, { match: { ...match.value } }, request.signal);
    if (attempt !== ticket) return;
    if (response.job?.kind !== 'refresh' || response.job.match.type !== match.value.type || response.job.match.pattern !== match.value.pattern) throw Error('Refresh preview does not match selection');
    job.value = response.job;
  } catch (reason) { if (attempt === ticket) error.value = reason; }
  finally { if (attempt === ticket) { phase.value = undefined; controller = undefined; } }
}
async function execute() {
  if (busy.value || job.value?.state !== 'ready') return;
  const request = new AbortController(), attempt = ++ticket, id = job.value.id;
  controller = request; phase.value = 'execute'; error.value = undefined;
  try {
    const response = await api<{ job: MaintenancePreview }>(`${appAPI(props.application)}/cache/refresh/${encodeURIComponent(id)}/execute`, {}, request.signal);
    if (attempt !== ticket) return;
    if (response.job?.id !== id || response.job.kind !== 'refresh') throw Error('Refresh job does not match selection');
    job.value = response.job;
    if (!running.value) emit('changed');
  } catch (reason) { if (attempt === ticket) error.value = reason; }
  finally { if (attempt === ticket) { phase.value = undefined; controller = undefined; } }
}
async function refreshFile(path: string) {
  if (busy.value) return;
  const request = new AbortController(), attempt = ++ticket;
  controller = request; phase.value = 'single'; error.value = undefined; single.value = undefined;
  try {
    const response = await api<{ item: RefreshItem }>(`${appAPI(props.application)}/cache/refresh`, { path: path.startsWith('/') ? path : `/${path}` }, request.signal);
    if (attempt === ticket) { single.value = response.item; emit('changed'); }
  } catch (reason) { if (attempt === ticket) error.value = reason; }
  finally { if (attempt === ticket) { phase.value = undefined; controller = undefined; } }
}
defineExpose({ refreshFile, busy });
onUnmounted(invalidate);
</script>
<template>
  <section class="panel cache-refresh-panel">
    <h2>{{ t('Refresh cached files') }}</h2>
    <p class="muted">{{ t('Refresh checks the selected files with their current upstream source. Cached files remain available according to the stale fallback setting.') }}</p>
    <p v-if="error" class="error refresh-error" role="alert">{{ errorText(error) }}</p>
    <p v-if="single" class="notice single-refresh-result" role="status"><code>{{ single.path }}</code> · {{ maintenanceStatus(single.status) }}<small v-if="single.reason">{{ single.reason }}</small></p>
    <p v-if="phase === 'single'" role="status">{{ t('Refreshing…') }}</p>
    <form @submit.prevent="plan"><MatcherInput v-model="match" :application="application" :disabled="running || phase === 'execute' || phase === 'single'" /><button class="secondary" :disabled="busy || !match.pattern">{{ phase === 'preview' ? t('Loading…') : t('Preview refresh') }}</button></form>
    <div v-if="job" class="refresh-review">
      <h3>{{ t('Refresh selection') }}</h3>
      <p>{{ job.match.type }} · <code>{{ job.match.pattern }}</code></p>
      <p>{{ t('{count} files · {size} logical bytes · {active} active', { count: job.selected_files, size: bytes(job.selected_bytes), active: job.active_files }) }}</p>
      <p>{{ t('Preview expires') }}: {{ localDate(job.expires_at) }}</p>
      <p class="refresh-job-state" role="status">{{ maintenanceStatus(job.state) }} · {{ t('{completed} completed · {failed} failed', { completed: job.completed_files, failed: job.failed_files }) }}</p>
      <p v-if="summary" class="notice refresh-summary" role="status">{{ t('{refreshed} refreshed · {unchanged} not modified · {stale} stale fallbacks · {failed} failed · {skipped} skipped', { refreshed: summary.refreshed, unchanged: summary.not_modified, stale: summary.stale_fallback, failed: summary.failed, skipped: summary.skipped }) }}</p>
      <div v-if="pollError" class="error" role="alert">{{ errorText(pollError) }}<IconButton type="button" class="secondary" :disabled="polling" @click="poll" icon="refresh" :label="t('Retry')" /></div>
      <p v-if="running" class="muted">{{ t('Refresh runs in the background. Leaving this page does not cancel the job.') }}</p>
      <PreviewFiles :application="application" kind="refresh" :job-id="job.id" :refresh-token="`${job.state}:${job.completed_files}:${job.failed_files}`" />
      <div v-if="job.state === 'ready'" class="form-actions"><button class="danger" :disabled="busy" @click="execute">{{ t('Confirm refresh for all selected files') }}</button><button type="button" class="secondary" :disabled="busy" @click="job = undefined">{{ t('Cancel') }}</button></div>
    </div>
  </section>
</template>

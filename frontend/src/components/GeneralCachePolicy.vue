<script setup lang="ts">
import { computed } from "vue";
import { appAPI } from "../bootstrap";
import { bytes } from "../api";
import type { AutoCleanupStatus, CachePolicy } from "../cachePolicy";
import { useSetting } from "../composables/useSetting";
import { usePageStatus } from "../composables/usePageStatus";
import { errorText, localDate, t } from "../i18n";
import MatcherInput from "./MatcherInput.vue";
import DurationInput from "./DurationInput.vue";
const props = defineProps<{ application: string }>();
const { draft, loading, saving, error, saved, load, save } = useSetting<CachePolicy>(computed(() => `${appAPI(props.application)}/cache/policy`));
const { status, loading: statusLoading, error: statusError, refresh: refreshStatus } = usePageStatus<AutoCleanupStatus>(computed(() => `${appAPI(props.application)}/cache/cleanup/status`));
const busy = computed(() => loading.value || saving.value);
type RuleList = "rules" | "auto_cleanup";
function move(kind: RuleList, index: number, delta: number) {
  const rules = draft.value?.[kind];
  if (!rules || index + delta < 0 || index + delta >= rules.length) return;
  [rules[index], rules[index + delta]] = [rules[index + delta]!, rules[index]!];
}
function add(kind: RuleList) {
  if (!draft.value || draft.value[kind].length >= 32) return;
  const match = { type: 'glob' as const, pattern: '/' };
  if (kind === 'rules') draft.value.rules.push({ match, ttl_seconds: 300 });
  else draft.value.auto_cleanup.push({ match, basis: 'last_access', age_seconds: 30 * 86400 });
}
</script>
<template>
  <section class="panel cache-policy">
    <h2>{{ t('Cache rules') }}</h2>
    <p v-if="error" class="error policy-error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t('Cache rules saved.') }}</p>
    <p v-if="loading" role="status">{{ t('Loading…') }}</p>
    <form @submit.prevent="save">
      <fieldset v-if="draft" :disabled="busy">
        <label class="checkbox-field"><input v-model="draft.stale_fallback" name="stale_fallback" type="checkbox" />{{ t('Use stale cache on origin failure') }}</label>
        <p class="muted small-text">{{ t('Enabled by default for all paths, including TTL 0. Disabling fallback returns an error on origin failure and keeps stored files.') }}</p>
        <section class="ttl-rules"><h3>{{ t('Path TTL rules') }}</h3>
          <p class="muted">{{ t('Rules run from top to bottom. The first matching path sets its TTL. Otherwise Cache-Control takes priority; the application default TTL applies only when Cache-Control is absent.') }}</p>
          <p class="muted">{{ t('TTL 0 checks the origin on every request and retains a complete copy. The stale fallback setting controls reuse on failure. Positive TTL rules can cache responses marked no-store or private by the origin.') }}</p>
          <div v-for="(rule, index) in draft.rules" :key="rule.id || index" class="policy-rule">
            <div class="rule-heading"><strong>{{ t('Rule {number}', { number: index + 1 }) }}</strong><div class="form-actions"><button type="button" class="secondary" :disabled="index === 0" @click="move('rules', index, -1)">{{ t('Move up') }}</button><button type="button" class="secondary" :disabled="index === draft.rules.length - 1" @click="move('rules', index, 1)">{{ t('Move down') }}</button><button type="button" class="secondary" @click="draft.rules.splice(index, 1)">{{ t('Remove rule') }}</button></div></div>
            <MatcherInput v-model="rule.match" :application="application" :disabled="busy" />
            <label>{{ t('TTL (seconds)') }}<input v-model.number="rule.ttl_seconds" name="rule_ttl" type="number" min="0" max="86400" step="1" required /></label>
          </div>
          <p v-if="!draft.rules.length" class="muted">{{ t('Cache-Control determines freshness; the application default TTL applies only when that header is absent.') }}</p>
          <button type="button" class="secondary" :disabled="draft.rules.length >= 32" @click="add('rules')">{{ t('Add TTL rule') }}</button>
        </section>
        <section class="auto-cleanup-rules"><h3>{{ t('Automatic cleanup rules') }}</h3>
          <p class="muted">{{ t('The first matching path rule owns the file. If its age is not reached, later rules do not apply.') }}</p>
          <p class="muted">{{ t('Saved rules run every 15 minutes for current active sources only. Each application pass scans at most 1,000 files and retires at most 100.') }}</p>
          <div v-for="(rule, index) in draft.auto_cleanup" :key="index" class="policy-rule">
            <div class="rule-heading"><strong>{{ t('Rule {number}', { number: index + 1 }) }}</strong><div class="form-actions"><button type="button" class="secondary" :disabled="index === 0" @click="move('auto_cleanup', index, -1)">{{ t('Move up') }}</button><button type="button" class="secondary" :disabled="index === draft.auto_cleanup.length - 1" @click="move('auto_cleanup', index, 1)">{{ t('Move down') }}</button><button type="button" class="secondary" @click="draft.auto_cleanup.splice(index, 1)">{{ t('Remove rule') }}</button></div></div>
            <MatcherInput v-model="rule.match" :application="application" :disabled="busy" />
            <label>{{ t('Select files by') }}<select v-model="rule.basis" name="rule_basis"><option value="fetched_at">{{ t('Fetched at') }}</option><option value="last_access">{{ t('Last accessed') }}</option></select></label>
            <p class="muted small-text">{{ rule.basis === 'fetched_at' ? t('Fetched-time cleanup can retire files that are still frequently accessed.') : t('Files accessed during cleanup are checked again and retained.') }}</p>
            <DurationInput v-model="rule.age_seconds" />
          </div>
          <p v-if="!draft.auto_cleanup.length" class="notice">{{ t('Automatic cleanup is disabled until rules are added and saved.') }}</p>
          <button type="button" class="secondary" :disabled="draft.auto_cleanup.length >= 32" @click="add('auto_cleanup')">{{ t('Add automatic cleanup rule') }}</button>
        </section>
        <p class="muted small-text">{{ t('Up to 32 rules per list. Save explicitly to apply these rules.') }}</p>
      </fieldset>
      <div class="form-actions"><button :disabled="busy || !draft">{{ saving ? t('Saving…') : t('Save cache rules') }}</button><button type="button" class="secondary" :disabled="busy" @click="load()">{{ t('Reload') }}</button></div>
    </form>
  </section>
  <section class="panel auto-cleanup-status">
    <div class="rule-heading"><h2>{{ t('Automatic cleanup service') }}</h2><button type="button" class="secondary" :disabled="statusLoading" @click="refreshStatus">{{ t('Refresh') }}</button></div>
    <p class="muted">{{ t('Service-wide last pass; includes all configured applications.') }}</p>
    <p v-if="statusError" class="error" role="alert">{{ errorText(statusError) }}</p>
    <p v-if="statusLoading && !status" role="status">{{ t('Loading…') }}</p>
    <template v-if="status">
      <p>{{ status.running ? t('Cleanup running') : t('Cleanup idle') }}</p>
      <dl class="cleanup-service-times"><dt>{{ t('Last attempt') }}</dt><dd>{{ localDate(status.last_attempt_at ?? undefined) }}</dd><dt>{{ t('Last successful pass') }}</dt><dd>{{ localDate(status.last_success_at ?? undefined) }}</dd><dt>{{ t('Last failed pass') }}</dt><dd>{{ localDate(status.last_error_at ?? undefined) }}</dd></dl>
      <p>{{ t('{apps} configured applications · {scanned} files scanned · {retired} files retired · {size}', { apps: status.configured_apps, scanned: status.scanned_files, retired: status.retired_files, size: bytes(status.retired_bytes) }) }}</p>
      <p>{{ t('Skipped: {accessed} accessed since preview; {changed} changed generations.', { accessed: status.skipped_accessed, changed: status.skipped_changed }) }}</p>
      <p>{{ t('{passes} total passes · {failures} failed passes', { passes: status.passes_total, failures: status.failures_total }) }}</p>
      <details v-if="status.last_error"><summary>{{ t('Technical details') }}</summary><pre>{{ status.last_error }}</pre></details>
    </template>
  </section>
</template>

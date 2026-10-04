<script setup lang="ts">
import SelectMenu from "../components/SelectMenu.vue";
import AutoRefresh from "../components/AutoRefresh.vue";
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { api, ApiError, bytes } from "../api";
import { appAPI } from "../bootstrap";
import { usePageStatus } from "../composables/usePageStatus";
import { useSourceEpoch } from "../composables/useSourceEpoch";
import SourceEpochSelect from "../components/SourceEpochSelect.vue";
import MatcherInput from "../components/MatcherInput.vue";
import type { MatcherSpec } from "../cachePolicy";
import { applicationRecord, applicationEnabled } from "../directory";
import PreviewFiles from "../components/PreviewFiles.vue";
import CacheRefresh from "../components/CacheRefresh.vue";
import type { MaintenancePreview } from "../maintenanceJobs";
import { errorText, localDate, t } from "../i18n";

interface CacheRow {
  generation_id: string;
  path: string;
  size_bytes: number;
  sha256: string;
  fetched_at: string;
  validated_at: string;
  last_access_at: string | null;
  fresh_until: string;
  etag: string;
  source_url?: string;
}
type Basis = "fetched_at" | "last_access";
interface Preview extends MaintenancePreview {
  basis: Basis;
  before: string;
}
interface Result {
  selected_files: number;
  retired_files: number;
  skipped_accessed: number;
  skipped_changed: number;
  retired_bytes: number;
}
const route = useRoute();
const application = computed(
  () => `${route.params.vendor}/${route.params.app}`,
);
const {
  sources,
  selected: sourceEpoch,
  query: sourceQuery,
  loading: sourcesLoading,
  error: sourcesError,
  load: loadSources,
} = useSourceEpoch(application);
const currentApp = computed(() =>
  applicationRecord.value?.key === application.value
    ? applicationRecord.value
    : undefined,
);
const currentSource = computed(() =>
  sources.value.find((source) => source.current),
);
const canRefresh = computed(
  () =>
    !sourceEpoch.value &&
    !!currentApp.value &&
    applicationEnabled(currentApp.value) &&
    currentSource.value?.active !== false,
);
const refreshPanel = ref<InstanceType<typeof CacheRefresh>>();
const {
  status: snapshot,
  loading,
  error: listError,
  automatic,
  refresh,
} = usePageStatus<{ items: CacheRow[] }>(
  computed(() => `${appAPI(application.value)}/cache${sourceQuery.value}`),
);
const basis = ref<Basis>("fetched_at"),
  beforeLocal = ref("");
const match = ref<MatcherSpec>({ type: "glob", pattern: "/" });
const preview = ref<Preview>(),
  result = ref<Result>(),
  cleanupError = ref<unknown>();
const phase = ref<"preview" | "execute">();
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const beforeUTC = computed(() => {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(beforeLocal.value)) return "";
  const date = new Date(beforeLocal.value);
  if (!Number.isFinite(date.getTime())) return "";
  // Reject normalized invalid dates and local times that do not exist at DST changes.
  const padded = (value: number) => String(value).padStart(2, "0");
  const roundtrip = `${date.getFullYear()}-${padded(date.getMonth() + 1)}-${padded(date.getDate())}T${padded(date.getHours())}:${padded(date.getMinutes())}`;
  return roundtrip === beforeLocal.value ? date.toISOString() : "";
});
const selection = computed(
  () =>
    `${application.value}\0${sourceEpoch.value}\0${basis.value}\0${beforeUTC.value}\0${JSON.stringify(match.value)}`,
);
let ticket = 0,
  controller: AbortController | undefined,
  previewSelection = "";
function invalidate() {
  ticket++;
  controller?.abort();
  controller = undefined;
  phase.value = undefined;
  preview.value = undefined;
  result.value = undefined;
  previewSelection = "";
}
watch([basis, beforeLocal], invalidate, { flush: "sync" });
watch(match, invalidate, { deep: true, flush: "sync" });
watch(
  sourceEpoch,
  () => {
    invalidate();
    cleanupError.value = undefined;
  },
  { flush: "sync" },
);
watch(
  application,
  () => {
    invalidate();
    basis.value = "fetched_at";
    beforeLocal.value = "";
    match.value = { type: "glob", pattern: "/" };
    cleanupError.value = undefined;
  },
  { flush: "sync" },
);
async function plan() {
  if (phase.value || !beforeUTC.value || !match.value.pattern) return;
  const request = new AbortController(),
    attempt = ++ticket;
  const requested = selection.value;
  controller = request;
  phase.value = "preview";
  cleanupError.value = undefined;
  preview.value = undefined;
  result.value = undefined;
  try {
    const response = await api<{ job: Preview }>(
      `${appAPI(application.value)}/cache/cleanup/preview${sourceQuery.value}`,
      {
        match: { ...match.value },
        basis: basis.value,
        before: beforeUTC.value,
      },
      request.signal,
    );
    if (attempt !== ticket || requested !== selection.value) return;
    if (
      !response.job ||
      response.job.basis !== basis.value ||
      response.job.match?.type !== match.value.type ||
      response.job.match?.pattern !== match.value.pattern ||
      new Date(response.job.before).getTime() !==
        new Date(beforeUTC.value).getTime()
    )
      throw Error("Cleanup preview does not match the requested selection");
    preview.value = response.job;
    previewSelection = requested;
  } catch (reason) {
    if (attempt === ticket) cleanupError.value = reason;
  } finally {
    if (attempt === ticket) {
      phase.value = undefined;
      controller = undefined;
    }
  }
}
async function execute() {
  const job = preview.value;
  if (
    phase.value ||
    !job ||
    job.state !== "ready" ||
    result.value ||
    previewSelection !== selection.value
  )
    return;
  if (new Date(job.expires_at).getTime() <= Date.now()) {
    preview.value = undefined;
    cleanupError.value = new ApiError({ code: "CLEANUP_INVALID" }, 409);
    return;
  }
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  phase.value = "execute";
  cleanupError.value = undefined;
  try {
    const response = await api<{ result: Result }>(
      `${appAPI(application.value)}/cache/cleanup/${encodeURIComponent(job.id)}/execute${sourceQuery.value}`,
      {},
      request.signal,
    );
    if (attempt !== ticket) return;
    if (!response.result) throw Error("Cleanup result unavailable");
    result.value = response.result;
    preview.value = { ...job, state: "done" };
    void refresh();
  } catch (reason) {
    if (attempt === ticket) {
      cleanupError.value = reason;
      if (
        reason instanceof ApiError &&
        (reason.status === 409 || reason.code === "CLEANUP_INVALID")
      )
        preview.value = undefined;
    }
  } finally {
    if (attempt === ticket) {
      phase.value = undefined;
      controller = undefined;
    }
  }
}
onUnmounted(invalidate);
</script>
<template>
  <div class="general-cache-page">
    <div class="page-heading">
      <div>
        <h2>{{ t("Cached files") }}</h2>
        <p class="muted">
          {{
            t(
              "Freshness determines revalidation. Cleanup retires stored files separately.",
            )
          }}
        </p>
      </div>
      <div class="form-actions">
        <AutoRefresh v-model="automatic" /><button
          class="secondary cache-refresh"
          :disabled="loading"
          @click="refresh"
        >
          {{ t("Refresh") }}
        </button>
      </div>
    </div>
    <SourceEpochSelect
      v-model="sourceEpoch"
      :sources="sources"
      :loading="sourcesLoading"
      :error="sourcesError"
      @reload="loadSources"
    />
    <div v-if="listError" class="error cache-list-error" role="alert">
      {{ errorText(listError)
      }}<small v-if="snapshot">{{
        t("Showing the last successful snapshot.")
      }}</small>
    </div>
    <p v-if="loading && !snapshot" role="status">{{ t("Loading…") }}</p>
    <section v-if="snapshot" class="panel">
      <div
        v-if="snapshot.items.length"
        class="table-wrap"
        tabindex="0"
        :aria-label="t('Cached files')"
      >
        <table class="cache-table">
          <thead>
            <tr>
              <th>{{ t("File") }}</th>
              <th>{{ t("Size") }}</th>
              <th>{{ t("Fetched at") }}</th>
              <th>{{ t("Last accessed") }}</th>
              <th>{{ t("Validated / fresh until") }}</th>
              <th>{{ t("Actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="file in snapshot.items" :key="file.generation_id">
              <td>
                <code>{{ file.path }}</code>
                <details>
                  <summary>{{ t("Technical details") }}</summary>
                  <dl>
                    <dt>{{ t("Generation") }}</dt>
                    <dd>
                      <code>{{ file.generation_id }}</code>
                    </dd>
                    <template v-if="file.source_url"
                      ><dt>{{ t("Actual source") }}</dt>
                      <dd>
                        <code>{{ file.source_url }}</code>
                      </dd></template
                    >
                    <dt>SHA-256</dt>
                    <dd>
                      <code>{{ file.sha256 }}</code>
                    </dd>
                    <dt>ETag</dt>
                    <dd>
                      <code>{{ file.etag || "—" }}</code>
                    </dd>
                  </dl>
                </details>
              </td>
              <td>{{ bytes(file.size_bytes) }}</td>
              <td>{{ localDate(file.fetched_at) }}</td>
              <td>{{ localDate(file.last_access_at ?? undefined) }}</td>
              <td>
                {{ localDate(file.validated_at) }}<br />{{
                  localDate(file.fresh_until)
                }}
              </td>
              <td>
                <button
                  type="button"
                  class="secondary refresh-file"
                  :disabled="!canRefresh || refreshPanel?.busy"
                  @click="refreshPanel?.refreshFile(file.path)"
                >
                  {{ t("Refresh file") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="empty">
        {{ t("No cached files yet. Files are fetched on demand.") }}
      </p>
      <p class="muted small-text">
        {{
          t(
            "Last access is stored conservatively in minute buckets. Times are shown in your local time zone.",
          )
        }}
      </p>
    </section>
    <CacheRefresh
      v-if="canRefresh"
      :key="`${application}:${currentSource?.epoch || currentApp?.source_epoch}`"
      ref="refreshPanel"
      :application="application"
      @changed="refresh"
    />
    <p v-else class="muted">
      {{
        t(
          "Refresh is available only for the current enabled source. Historical files remain available for cleanup.",
        )
      }}
    </p>
    <section class="panel time-cleanup">
      <h2>{{ t("Time-based cleanup") }}</h2>
      <p v-if="cleanupError" class="error cleanup-error" role="alert">
        {{ errorText(cleanupError) }}
      </p>
      <form @submit.prevent="plan">
        <fieldset :disabled="phase === 'execute'">
          <MatcherInput
            v-model="match"
            :application="application"
            :disabled="phase === 'execute'"
          />
          <label
            >{{ t("Select files by")
            }}<SelectMenu
              v-model="basis"
              name="basis"
              :label="t('Select files by')"
              :options="[
                { value: 'fetched_at', label: t('Fetched at') },
                { value: 'last_access', label: t('Last accessed') },
              ]"
          /></label>
          <p class="muted">
            {{
              basis === "fetched_at"
                ? t(
                    "Fetched-time cleanup can retire files that are still frequently accessed.",
                  )
                : t(
                    "Files accessed after the preview are checked again and skipped when you confirm.",
                  )
            }}
          </p>
          <label
            >{{ t("Before local time") }} · {{ timeZone
            }}<input
              v-model="beforeLocal"
              name="before"
              type="datetime-local"
              step="60"
              required
          /></label>
          <p v-if="beforeUTC" class="cleanup-cutoff">
            {{ t("UTC cutoff") }}:
            <time :datetime="beforeUTC">{{ beforeUTC }}</time>
          </p>
        </fieldset>
        <button
          class="secondary"
          :disabled="!!phase || !beforeUTC || !match.pattern"
        >
          {{ phase === "preview" ? t("Loading…") : t("Preview cleanup") }}
        </button>
      </form>
      <div v-if="preview" class="cleanup-review">
        <h3>{{ t("Confirm this preview") }}</h3>
        <p>
          {{ preview.match.type }} · <code>{{ preview.match.pattern }}</code>
        </p>
        <p>
          {{
            t("{count} files · {size} logical bytes · {active} active", {
              count: preview.selected_files,
              size: bytes(preview.selected_bytes),
              active: preview.active_files,
            })
          }}
        </p>
        <p>{{ t("Preview expires") }}: {{ localDate(preview.expires_at) }}</p>
        <p class="muted">
          {{
            t(
              "Only previewed generations are retired. Later generations remain available. Existing readers and writers drain before disk space is reclaimed.",
            )
          }}
        </p>
        <PreviewFiles
          :application="application"
          kind="cleanup"
          :job-id="preview.id"
          :source-query="sourceQuery"
          :refresh-token="result ? 'done' : ''"
        />
        <div v-if="!result" class="form-actions">
          <button
            class="danger"
            :disabled="!!phase || preview.state !== 'ready'"
            @click="execute"
          >
            {{ t("Confirm cleanup") }}</button
          ><button
            class="secondary"
            :disabled="!!phase"
            @click="preview = undefined"
          >
            {{ t("Cancel") }}
          </button>
        </div>
      </div>
      <div v-if="result" class="notice cleanup-result" role="status">
        <p>
          {{
            t("Retired {count} of {selected} files · {size} logical bytes", {
              count: result.retired_files,
              selected: result.selected_files,
              size: bytes(result.retired_bytes),
            })
          }}
        </p>
        <p>
          {{
            t(
              "Skipped: {accessed} accessed since preview; {changed} changed generations.",
              {
                accessed: result.skipped_accessed,
                changed: result.skipped_changed,
              },
            )
          }}
        </p>
        <p>
          {{
            t(
              "Cleanup executed; space is reclaimed after existing readers and writers finish",
            )
          }}
        </p>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { api, bytes, isCancellation } from "../api";
import { appAPI } from "../bootstrap";
import { useConfiguration } from "../composables/useConfiguration";
import OverrideControl from "./OverrideControl.vue";
import PageNavigation from "./PageNavigation.vue";
import { errorText, t, type Message } from "../i18n";
function translate(value: string) {
  return t(value as Message);
}
const props = defineProps<{ application: string }>();
const {
  draft,
  configuration,
  touched,
  unsets,
  loading,
  saving,
  error,
  saved,
  save,
  mark,
  restore,
} = useConfiguration<{ retention: { enabled: boolean; keep_latest: number } }>(
  computed(() => `apps/${props.application}/configuration`),
  "",
  ["retention"],
);
type Version = {
  version: string;
  reasons: string[];
  bytes: number;
  selected: boolean;
};
type Preview = {
  id: string;
  selected_versions: number;
  logical_bytes: number;
  reclaimable_bytes: number;
  expires: string;
};
type Result = {
  retired_versions: number;
  logical_bytes: number;
  skipped: Record<string, string>;
};
type Status = {
  outcome?: string;
  reason?: string;
  attempt?: string;
  retired_versions?: number;
  logical_bytes?: number;
  next_check?: string;
};
const preview = ref<Preview>(),
  result = ref<Result>(),
  status = ref<Status>({}),
  items = ref<Version[]>([]),
  page = ref(1),
  total = ref(0),
  totalPages = ref(1),
  busy = ref(false),
  requestError = ref<unknown>(),
  confirmEnable = ref(false);
let ticket = 0,
  controller: AbortController | undefined,
  statusTicket = 0,
  statusController: AbortController | undefined,
  enableConfirmed = false;
function invalidate() {
  enableConfirmed = false;
  ticket++;
  controller?.abort();
  controller = undefined;
  preview.value = undefined;
  items.value = [];
  result.value = undefined;
  busy.value = false;
  requestError.value = undefined;
  confirmEnable.value = false;
}
watch(
  () => [
    props.application,
    configuration.value?.revision,
    JSON.stringify(draft.value?.retention),
  ],
  invalidate,
  { flush: "sync" },
);
async function statusLoad() {
  const app = props.application,
    attempt = ++statusTicket;
  statusController?.abort();
  statusController = new AbortController();
  try {
    const value = await api<Status>(
      `${appAPI(app)}/retention/status`,
      undefined,
      statusController.signal,
    );
    if (attempt === statusTicket && app === props.application)
      status.value = value;
  } catch (reason) {
    if (
      attempt === statusTicket &&
      app === props.application &&
      !isCancellation(reason)
    )
      requestError.value = reason;
  }
}
watch(
  () => props.application,
  () => {
    status.value = {};
    void statusLoad();
  },
  { immediate: true },
);
async function request(action: "preview" | "execute" | "page", requested = 1) {
  if (busy.value || !configuration.value || !draft.value) return;
  const request = new AbortController(),
    attempt = ++ticket,
    app = props.application;
  controller = request;
  busy.value = true;
  requestError.value = undefined;
  try {
    if (action === "preview") {
      const value = await api<Preview>(
        `${appAPI(app)}/retention/preview`,
        { revision: configuration.value.revision },
        request.signal,
      );
      if (attempt !== ticket) return;
      preview.value = value;
      requested = 1;
      result.value = undefined;
    }
    if (action === "execute" && preview.value) {
      const value = await api<Result>(
        `${appAPI(app)}/retention/${preview.value.id}/execute`,
        {},
        request.signal,
      );
      if (attempt !== ticket) return;
      result.value = value;
      void statusLoad();
    }
    if (action !== "execute" && preview.value) {
      const value = await api<{
        items: Version[];
        total: number;
        total_pages: number;
      }>(
        `${appAPI(app)}/retention/${preview.value.id}/items?page=${requested}`,
        undefined,
        request.signal,
      );
      if (attempt !== ticket) return;
      items.value = value.items;
      page.value = requested;
      total.value = value.total;
      totalPages.value = value.total_pages;
    }
  } catch (reason) {
    if (attempt === ticket && !isCancellation(reason))
      requestError.value = reason;
  } finally {
    if (attempt === ticket) {
      busy.value = false;
      controller = undefined;
    }
  }
}
function enabled(event: Event) {
  if (!draft.value) return;
  if ((event.target as HTMLInputElement).checked) {
    confirmEnable.value = true;
    (event.target as HTMLInputElement).checked = false;
  } else {
    draft.value.retention.enabled = false;
    mark("retention");
  }
}
async function formSave() {
  if (
    draft.value?.retention.enabled &&
    !(configuration.value?.effective.retention as { enabled: boolean })
      ?.enabled &&
    !enableConfirmed
  ) {
    confirmEnable.value = true;
    return;
  }
  await save();
}
function enable() {
  const already = draft.value?.retention.enabled;
  if (draft.value) {
    draft.value.retention.enabled = true;
    mark("retention");
  }
  confirmEnable.value = false;
  enableConfirmed = true;
  if (already) void formSave();
}
onUnmounted(() => {
  invalidate();
  statusTicket++;
  statusController?.abort();
});
</script>
<template>
  <section class="panel">
    <h2>{{ t("Keep latest cached versions") }}</h2>
    <p
      class="muted"
      :title="
        t(
          'Latest N cached versions are protected first, plus every declared channel, active reader or writer, and versions that cannot be compared. Historical sources are untouched.',
        )
      "
    >
      {{
        t(
          "Old cached binaries are deleted and can be fetched again. Configuration and download statistics are retained.",
        )
      }}
    </p>
    <p v-if="error || requestError" class="error" role="alert">
      {{ errorText(error || requestError) }}
    </p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form @submit.prevent="formSave">
      <label
        ><input
          type="checkbox"
          :checked="draft?.retention.enabled"
          :disabled="loading || saving || !draft"
          @change="enabled"
        />{{ t("Automatically clean old cached versions") }}</label
      >
      <label
        >{{ t("Versions to keep")
        }}<input
          type="number"
          min="1"
          max="1000"
          required
          :value="draft?.retention.keep_latest"
          :disabled="loading || saving || !draft"
          @input="
            draft &&
            ((draft.retention.keep_latest = Number(
              ($event.target as HTMLInputElement).value,
            )),
            mark('retention'))
          "
      /></label>
      <OverrideControl
        :configuration="configuration"
        path="retention"
        :custom="touched.has('retention')"
        :restored="unsets.has('retention')"
        :disabled="loading || saving"
        @restore="restore('retention')"
        @customize="mark('retention')"
      />
      <button :disabled="loading || saving">{{ t("Save") }}</button>
    </form>
    <div v-if="confirmEnable" class="cleanup-review" role="alert">
      <p>
        {{
          t(
            "Enable automatic deletion of old cached binaries? The first check occurs on the next scheduled cycle.",
          )
        }}
      </p>
      <button class="danger" @click="enable">
        {{ t("Enable automatic cleanup") }}</button
      ><button class="secondary" @click="confirmEnable = false">
        {{ t("Cancel") }}
      </button>
    </div>
    <p v-if="status.outcome">
      {{ t("Recent retention result") }}: {{ translate(status.outcome) }} ·
      {{ status.reason ? translate(status.reason) : "" }} ·
      {{ status.retired_versions || 0 }} · {{ bytes(status.logical_bytes || 0)
      }}<br />{{ t("Next check") }}: {{ status.next_check }}
    </p>
    <button
      class="secondary"
      :disabled="
        busy || loading || saving || touched.size > 0 || unsets.size > 0
      "
      @click="request('preview')"
    >
      {{ t("Preview cleanup") }}
    </button>
    <div v-if="preview" class="cleanup-review">
      <p>
        {{ t("Confirm this preview") }}: {{ preview.selected_versions }} ·
        {{ bytes(preview.logical_bytes) }} ·
        {{
          t("Estimated reclaimable complete cache: {size}", {
            size: bytes(preview.reclaimable_bytes),
          })
        }}
      </p>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("Version") }}</th>
              <th>{{ t("Reason") }}</th>
              <th>{{ t("Size") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.version">
              <td>{{ item.version }}</td>
              <td>
                {{ item.reasons.map((reason) => translate(reason)).join(", ") }}
              </td>
              <td>{{ bytes(item.bytes) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <PageNavigation
        :label="t('Versions')"
        :page="page"
        :previous="page > 1"
        :next="page < totalPages"
        :loading="busy"
        :total="total"
        :total-pages="totalPages"
        @previous="request('page', page - 1)"
        @next="request('page', page + 1)"
        @go="request('page', $event)"
        @refresh="request('page', page)"
      />
      <button
        class="danger"
        :disabled="busy || !!result"
        @click="request('execute')"
      >
        {{ t("Clean now using this policy") }}</button
      ><button class="secondary" :disabled="busy" @click="invalidate">
        {{ t("Cancel") }}
      </button>
      <p v-if="result" role="status">
        {{
          t(
            "Cleanup executed; space is reclaimed after existing readers and writers finish",
          )
        }}
        · {{ result.retired_versions }} · {{ bytes(result.logical_bytes)
        }}<span v-for="(reason, version) in result.skipped" :key="version">
          · {{ version }}: {{ translate(reason) }}</span
        >
      </p>
    </div>
  </section>
</template>

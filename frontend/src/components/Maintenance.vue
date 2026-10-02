<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { api, bytes, type CleanupPreview } from "../api";
import { appAPI } from "../bootstrap";
import { useSetting } from "../composables/useSetting";
import { errorText, t, type Message } from "../i18n";
const props = defineProps<{ application: string }>();
const emit = defineEmits<{ error: [unknown]; changed: [] }>();
const path = computed(() => `${appAPI(props.application)}/settings`);
const {
  draft,
  loading,
  saving,
  error,
  load,
  save: saveSetting,
  saved,
} = useSetting<{ channel_ttl_seconds: number }>(path);
const ttl = computed({
  get: () => draft.value?.channel_ttl_seconds,
  set: (value) => {
    if (draft.value && value !== undefined)
      draft.value.channel_ttl_seconds = value;
  },
});
const minimum = ref(""),
  preview = ref<CleanupPreview>(),
  cleanupBusy = ref(false),
  cleanupError = ref<unknown>(),
  message = ref<Message>();
const busy = computed(() => saving.value || cleanupBusy.value);
let ticket = 0,
  controller: AbortController | undefined,
  previewVersion = "";
function reset() {
  ticket++;
  controller?.abort();
  controller = undefined;
  preview.value = undefined;
  minimum.value = "";
  cleanupError.value = undefined;
  message.value = undefined;
  cleanupBusy.value = false;
}
watch(() => props.application, reset, { flush: "sync" });
watch(minimum, () => {
  preview.value = undefined;
});
async function save() {
  message.value = undefined;
  await saveSetting();
}
async function plan() {
  if (cleanupBusy.value || !minimum.value) return;
  const request = new AbortController(),
    attempt = ++ticket,
    app = props.application,
    requested = minimum.value;
  controller = request;
  cleanupBusy.value = true;
  cleanupError.value = undefined;
  preview.value = undefined;
  try {
    const result = await api<CleanupPreview>(
      `${appAPI(app)}/cleanup/preview`,
      { minimum_version: requested },
      request.signal,
    );
    if (
      attempt === ticket &&
      app === props.application &&
      requested === minimum.value
    ) {
      preview.value = result;
      previewVersion = requested;
      message.value = "Review the selected generations before confirming";
    }
  } catch (reason) {
    if (attempt === ticket) cleanupError.value = reason;
  } finally {
    if (attempt === ticket) {
      cleanupBusy.value = false;
      controller = undefined;
    }
  }
}
async function clean() {
  const selected = preview.value;
  if (!selected || cleanupBusy.value || minimum.value !== previewVersion)
    return;
  const request = new AbortController(),
    attempt = ++ticket,
    app = props.application;
  controller = request;
  cleanupBusy.value = true;
  cleanupError.value = undefined;
  try {
    await api(
      `${appAPI(app)}/cleanup/${encodeURIComponent(selected.job.ID)}/execute`,
      {},
      request.signal,
    );
    if (attempt === ticket) {
      preview.value = undefined;
      message.value =
        "Cleanup executed; space is reclaimed after existing readers and writers finish";
      emit("changed");
    }
  } catch (reason) {
    if (attempt === ticket) cleanupError.value = reason;
  } finally {
    if (attempt === ticket) {
      cleanupBusy.value = false;
      controller = undefined;
    }
  }
}
onUnmounted(reset);
</script>
<template>
  <div class="maintenance-stack">
    <p v-if="message || saved" class="notice" role="status">
      {{ message ? t(message) : t("Channel TTL saved") }}
    </p>
    <section class="panel">
      <h2>{{ t("Metadata freshness") }}</h2>
      <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
      <p v-if="loading" role="status">{{ t("Loading…") }}</p>
      <form class="ttl-form" @submit.prevent="save">
        <label
          >{{ t("Channel TTL (seconds)")
          }}<input
            v-model.number="ttl"
            type="number"
            min="1"
            max="86400"
            required
            :disabled="loading || busy || !draft"
        /></label>
        <div class="form-actions">
          <button :disabled="busy || loading || ttl === undefined">
            {{ busy ? t("Saving…") : t("Save TTL") }}</button
          ><button
            class="secondary"
            type="button"
            :disabled="busy || loading"
            @click="load()"
          >
            {{ t("Reload") }}
          </button>
        </div>
      </form>
    </section>
    <section class="panel">
      <h2>{{ t("Version cleanup") }}</h2>
      <p class="muted">
        {{
          t(
            "Only previewed generations are retired. Later generations remain available. Existing readers and writers drain before disk space is reclaimed.",
          )
        }}
      </p>
      <p v-if="cleanupError" class="error" role="alert">
        {{ errorText(cleanupError) }}
      </p>
      <form class="cleanup" @submit.prevent="plan">
        <label
          >{{ t("Minimum version to keep")
          }}<input
            v-model="minimum"
            placeholder="1.0.0"
            required
            :disabled="busy && !!preview"
            @input="preview = undefined" /></label
        ><button class="secondary" :disabled="busy || loading">
          {{ busy ? t("Loading…") : t("Preview cleanup") }}
        </button>
      </form>
      <div v-if="preview" class="cleanup-review">
        <h3>{{ t("Confirm this preview") }}</h3>
        <p>
          {{
            t("{count} generations · {size} logical bytes · {active} active", {
              count: preview.job.Selected?.length || 0,
              size: bytes(preview.logical_bytes),
              active: preview.active,
            })
          }}
        </p>
        <p class="cleanup-reclaimable">
          {{
            t("Estimated reclaimable complete cache: {size}", {
              size: bytes(preview.reclaimable_blob_bytes),
            })
          }}
        </p>
        <p v-if="preview.unknown_versions?.length">
          {{ t("Unknown versions retained") }}:
          {{ preview.unknown_versions.join(", ") }}
        </p>
        <details>
          <summary>{{ t("Selected generations") }}</summary>
          <pre>{{ JSON.stringify(preview.job.Selected, null, 2) }}</pre>
        </details>
        <div class="form-actions">
          <button class="danger" :disabled="busy" @click="clean">
            {{ t("Confirm cleanup") }}</button
          ><button
            class="secondary"
            :disabled="busy"
            @click="
              preview = undefined;
              message = undefined;
            "
          >
            {{ t("Cancel") }}
          </button>
        </div>
      </div>
    </section>
  </div>
</template>

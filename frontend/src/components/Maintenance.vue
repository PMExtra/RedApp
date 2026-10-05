<script setup lang="ts">
import DisclosureIcon from "./DisclosureIcon.vue";
import { computed, onUnmounted, ref, watch } from "vue";
import { api, bytes, type CleanupPreview } from "../api";
import { appAPI } from "../bootstrap";
import { useSourceEpoch } from "../composables/useSourceEpoch";
import SourceEpochSelect from "./SourceEpochSelect.vue";
import { errorText, t, type Message } from "../i18n";
const props = defineProps<{ application: string }>();
const emit = defineEmits<{ error: [unknown]; changed: [] }>();
const application = computed(() => props.application);
const {
  sources,
  selected: sourceEpoch,
  query: sourceQuery,
  loading: sourcesLoading,
  error: sourcesError,
  load: loadSources,
} = useSourceEpoch(application);
const minimum = ref(""),
  preview = ref<CleanupPreview>(),
  cleanupBusy = ref(false),
  cleanupError = ref<unknown>(),
  message = ref<Message>();
const busy = cleanupBusy;
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
watch(sourceEpoch, reset, { flush: "sync" });
watch(minimum, () => {
  preview.value = undefined;
});
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
      `${appAPI(app)}/cleanup/preview${sourceQuery.value}`,
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
      `${appAPI(app)}/cleanup/${encodeURIComponent(selected.job.ID)}/execute${sourceQuery.value}`,
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
    <p v-if="message" class="notice" role="status">
      {{ t(message) }}
    </p>
    <section class="panel">
      <h2>{{ t("Version cleanup") }}</h2>
      <SourceEpochSelect
        v-model="sourceEpoch"
        :sources="sources"
        :loading="sourcesLoading"
        :error="sourcesError"
        @reload="loadSources"
      />
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
        ><button class="secondary" :disabled="busy">
          {{ busy ? t("Loading…") : t("Preview cleanup") }}
        </button>
      </form>
      <div v-if="preview" class="cleanup-review">
        <h3>{{ t("Confirm this preview") }}</h3>
        <p>
          {{
            t(
              "{count} generations · {size} logical bytes · {active} active",
              {
                count: preview.job.Selected?.length || 0,
                size: bytes(preview.logical_bytes),
                active: preview.active,
              },
            )
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
          <summary><DisclosureIcon />{{ t("Selected generations") }}</summary>
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

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import { api, bytes, type CleanupPreview } from "../api";
import { t, type Message } from "../i18n";
const emit = defineEmits<{ error: [unknown]; changed: [] }>();
const ttl = ref<number>(),
  minimum = ref(""),
  preview = ref<CleanupPreview>(),
  busy = ref(false),
  message = ref<Message>(),
  loading = ref(true);
let controller: AbortController | undefined,
  disposed = false,
  previewVersion = "";
watch(minimum, () => {
  preview.value = undefined;
});
async function load() {
  if (controller) return;
  loading.value = true;
  controller = new AbortController();
  try {
    const data = await api<{ latest_ttl_seconds: number }>(
      "settings",
      undefined,
      controller.signal,
    );
    if (!disposed) ttl.value = data.latest_ttl_seconds;
  } catch (error) {
    if (!disposed) emit("error", error);
  } finally {
    loading.value = false;
    controller = undefined;
  }
}
async function run(action: (signal: AbortSignal) => Promise<void>) {
  if (busy.value || disposed || controller) return;
  busy.value = true;
  message.value = undefined;
  controller = new AbortController();
  try {
    await action(controller.signal);
  } catch (error) {
    if (!disposed) emit("error", error);
  } finally {
    busy.value = false;
    controller = undefined;
  }
}
async function save() {
  await run(async (signal) => {
    await api("settings", { latest_ttl_seconds: ttl.value }, signal);
    if (!disposed) message.value = "latest TTL saved";
  });
}
async function plan() {
  if (busy.value || loading.value) return;
  preview.value = undefined;
  const requested = minimum.value;
  await run(async (signal) => {
    const result = await api<CleanupPreview>(
      "cleanup/preview",
      { minimum_version: requested },
      signal,
    );
    if (!disposed && requested === minimum.value) {
      preview.value = result;
      previewVersion = requested;
      message.value = "Review the selected generations before confirming";
    }
  });
}
async function clean() {
  const selected = preview.value;
  if (!selected || minimum.value !== previewVersion) return;
  await run(async (signal) => {
    await api("cleanup/execute", { cleanup_id: selected.job.ID }, signal);
    if (!disposed) {
      preview.value = undefined;
      message.value =
        "Cleanup executed; space is reclaimed after existing readers and writers finish";
      emit("changed");
    }
  });
}
onMounted(load);
onUnmounted(() => {
  disposed = true;
  controller?.abort();
});
</script>
<template>
  <div class="maintenance-stack">
    <p v-if="message" class="notice" role="status">{{ t(message) }}</p>
    <section class="panel">
      <h2>{{ t("Metadata freshness") }}</h2>
      <p class="muted">
        {{
          t(
            "Only latest metadata expires automatically. Other valid versions remain cached.",
          )
        }}
      </p>
      <p v-if="loading" role="status">{{ t("Loading…") }}</p>
      <form class="ttl-form" @submit.prevent="save">
        <label
          >{{ t("latest TTL (seconds)")
          }}<input
            v-model.number="ttl"
            type="number"
            min="1"
            max="86400"
            required
            :disabled="loading || busy"
        /></label>
        <div class="form-actions">
          <button :disabled="busy || loading || ttl === undefined">
            {{ busy ? t("Saving…") : t("Save TTL") }}</button
          ><button
            class="secondary"
            type="button"
            :disabled="busy || loading"
            @click="load"
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
      <form class="cleanup" @submit.prevent="plan">
        <label
          >{{ t("Minimum version to keep")
          }}<input
            v-model="minimum"
            placeholder="0.150.0"
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

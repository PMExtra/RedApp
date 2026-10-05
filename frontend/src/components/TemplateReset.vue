<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { ref, watch, onUnmounted, computed } from "vue";
import { confirmDirtyDrafts } from "../composables/useDirtyDraft";
import { api, isCancellation } from "../api";
import type { ManagedApplication } from "../directory";
import type { LocalizedText } from "../site";
import { t, errorText, type Message } from "../i18n";
import SwitchControl from "./SwitchControl.vue";
const props = defineProps<{ application: string; kind?: "app" | "vendor" }>();
const emit = defineEmits<{ saved: [] }>();
interface Preview {
  template: { application: ManagedApplication; instructions: LocalizedText };
  current: ManagedApplication;
  instructions: LocalizedText & { revision: number };
  groups: string[];
}
const preview = ref<Preview>(),
  selected = ref<string[]>([]),
  error = ref<unknown>(),
  loading = ref(false),
  review = ref(false),
  saved = ref(false);
let ticket = 0,
  controller: AbortController | undefined;
const labels: Record<string, Message> = {
  metadata: "Name and description",
  icon: "Icon",
  enabled: "Enabled",
  source: "Sources and delivery",
  cache: "Cache settings",
  instructions_en: "English instructions",
  instructions_zh: "Chinese instructions",
};
function text(group: string, after: boolean): string {
  const value = preview.value;
  if (!value) return "";
  const app = after ? value.template.application : value.current,
    instructions = after ? value.template.instructions : value.instructions;
  switch (group) {
    case "metadata":
      return JSON.stringify(
        { name: app.name, description: app.description },
        null,
        2,
      );
    case "icon":
      return app.icon || "—";
    case "enabled":
      return app.enabled ? t("On") : t("Off");
    case "source":
      return JSON.stringify(
        {
          base_url: app.base_url,
          base_urls: app.base_urls,
          source_strategy: app.source_strategy,
        },
        null,
        2,
      );
    case "cache":
      return String(app.cache_ttl_seconds) + " s";
    case "instructions_en":
      return instructions.en;
    case "instructions_zh":
      return instructions["zh-CN"];
    default:
      return "";
  }
}
const groups = computed(() => preview.value?.groups || []);
async function load() {
  controller?.abort();
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  loading.value = true;
  preview.value = undefined;
  error.value = undefined;
  selected.value = [];
  review.value = false;
  saved.value = false;
  try {
    const value = await api<Preview>(
      `${props.kind === "vendor" ? "vendors" : "apps"}/${props.application}/template`,
      undefined,
      request.signal,
    );
    if (attempt === ticket) preview.value = value;
  } catch (e) {
    if (attempt === ticket && !isCancellation(e)) error.value = e;
  } finally {
    if (attempt === ticket) loading.value = false;
  }
}
function toggle(group: string, value: boolean) {
  selected.value = value
    ? [...selected.value, group]
    : selected.value.filter((v) => v !== group);
  review.value = false;
}
async function save() {
  if (
    !preview.value ||
    !selected.value.length ||
    loading.value ||
    !review.value ||
    !confirmDirtyDrafts()
  )
    return;
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  loading.value = true;
  error.value = undefined;
  try {
    await api(
      `${props.kind === "vendor" ? "vendors" : "apps"}/${props.application}/template`,
      {
        revision: preview.value.current.revision,
        instructions_revision: preview.value.instructions.revision,
        groups: selected.value,
      },
      request.signal,
    );
    if (attempt === ticket) {
      emit("saved");
      await load();
      saved.value = true;
    }
  } catch (e) {
    if (attempt === ticket && !isCancellation(e)) error.value = e;
  } finally {
    if (attempt === ticket) loading.value = false;
  }
}
watch(() => props.application, load, { immediate: true });
onUnmounted(() => {
  ticket++;
  controller?.abort();
});
</script>
<template>
  <section class="panel template-reset">
    <h2>{{ t("Reset selected fields to template") }}</h2>
    <p class="muted">
      {{
        t(
          "Select fields, review the differences, then save. Identity and provider stay fixed; stored files and history are retained.",
        )
      }}
    </p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <div class="template-groups">
      <SwitchControl
        v-for="group in groups"
        :key="group"
        :model-value="selected.includes(group)"
        :label="t(labels[group]!)"
        :disabled="loading"
        @update:model-value="toggle(group, $event)"
      />
    </div>
    <div v-if="review" class="template-diff">
      <section v-for="group in selected" :key="group">
        <h3>{{ t(labels[group]!) }}</h3>
        <div class="two-columns">
          <div>
            <h4>{{ t("Current value") }}</h4>
            <pre>{{ text(group, false) }}</pre>
          </div>
          <div>
            <h4>{{ t("Template value") }}</h4>
            <pre>{{ text(group, true) }}</pre>
          </div>
        </div>
      </section>
    </div>
    <div class="form-actions">
      <button
        v-if="!review"
        :disabled="loading || !selected.length"
        @click="review = true"
      >
        {{ t("Review differences") }}</button
      ><button v-else :disabled="loading || !selected.length" @click="save">
        {{ t("Save selected fields") }}</button
      ><IconButton
        class="secondary"
        :disabled="loading"
        @click="load"
        icon="refresh"
        :label="t('Reload')"
      />
    </div>
  </section>
</template>

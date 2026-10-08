<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { computed } from "vue";
import { useConfiguration } from "../composables/useConfiguration";
import OverrideControl from "./OverrideControl.vue";
import { invalidateBootstrap, loadBootstrap } from "../bootstrap";
import { errorText, t } from "../i18n";
import type { LocalizedText } from "../site";
const variableHint = computed(() =>
  t(
    "Use {{base_url}} for the public address, {{app_path}} for /vendor/app, and {{latest_version}} for the latest known version or <version> when unavailable.",
  ),
);
const props = defineProps<{ application: string; readonly?: boolean }>();
const {
  draft,
  configuration,
  touched,
  unsets,
  loading,
  saving,
  error,
  saved,
  load,
  save,
  mark,
  restore,
} = useConfiguration<LocalizedText>(
  computed(() => `apps/${props.application}/configuration`),
  "instructions",
  ["en", "zh-CN"],
  () => {
    invalidateBootstrap();
    void loadBootstrap();
  },
);
</script>
<template>
  <section class="panel application-instructions-editor">
    <h2>{{ t("Usage instructions") }}</h2>
    <p class="muted">
      {{
        t(
          "Markdown, HTML, JavaScript and external resources are supported. Only administrators can edit these instructions.",
        )
      }}
    </p>
    <p class="muted small-text">{{ variableHint }}</p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form @submit.prevent="save">
      <fieldset :disabled="readonly || loading || saving">
        <div v-if="draft" class="two-columns">
          <label
            >English<textarea
              v-model="draft.en"
              @input="mark('en')"
              name="instructions-en"
              lang="en"
              maxlength="12000"
              rows="9" /><OverrideControl
              :configuration="configuration"
              path="instructions.en"
              :custom="touched.has('en')"
              :restored="unsets.has('en')"
              @restore="restore('en')"
              @customize="mark('en')" /></label
          ><label
            >简体中文<textarea
              v-model="draft['zh-CN']"
              @input="mark('zh-CN')"
              name="instructions-zh-CN"
              lang="zh-CN"
              maxlength="12000"
              rows="9"
            />
            <OverrideControl
              :configuration="configuration"
              path="instructions.zh-CN"
              :custom="touched.has('zh-CN')"
              :restored="unsets.has('zh-CN')"
              @restore="restore('zh-CN')"
              @customize="mark('zh-CN')"
            />
          </label>
        </div>
        <div class="form-actions">
          <button :disabled="!draft">
            {{ saving ? t("Saving…") : t("Save instructions") }}</button
          ><IconButton
            type="button"
            class="secondary"
            @click="load()"
            icon="refresh"
            :label="t('Reload')"
          />
        </div>
      </fieldset>
    </form>
  </section>
</template>

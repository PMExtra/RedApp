<script setup lang="ts">
import SelectMenu from "./SelectMenu.vue";
import { computed, onUnmounted, ref, watch, watchEffect } from "vue";
import { api } from "../api";
import { appAPI } from "../bootstrap";
import type { MatcherSpec } from "../cachePolicy";
import { errorText, t } from "../i18n";
const props = defineProps<{
  application: string;
  modelValue: MatcherSpec;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [MatcherSpec] }>();
const sample = ref("/releases/example.zip"),
  busy = ref(false),
  error = ref<unknown>();
const result = ref<{ matches: boolean; canonical_path: string }>();
const patternInput = ref<HTMLInputElement>();
const patternTooLong = computed(
  () => new TextEncoder().encode(props.modelValue.pattern).byteLength > 1024,
);
watchEffect(() =>
  patternInput.value?.setCustomValidity(
    patternTooLong.value
      ? t("Patterns may contain at most 1,024 UTF-8 bytes.")
      : "",
  ),
);
let ticket = 0,
  controller: AbortController | undefined;
function invalidate() {
  ticket++;
  controller?.abort();
  controller = undefined;
  busy.value = false;
  error.value = undefined;
  result.value = undefined;
}
watch(
  () => [
    props.application,
    props.modelValue.type,
    props.modelValue.pattern,
    sample.value,
  ],
  invalidate,
  { flush: "sync" },
);
function changeType(value: string) {
  emit("update:modelValue", {
    ...props.modelValue,
    type: value as MatcherSpec["type"],
  });
}
async function test() {
  if (
    busy.value ||
    props.disabled ||
    patternTooLong.value ||
    !sample.value ||
    !props.modelValue.pattern
  )
    return;
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  busy.value = true;
  error.value = undefined;
  result.value = undefined;
  try {
    const response = await api<{ matches: boolean; canonical_path: string }>(
      `${appAPI(props.application)}/cache/match`,
      { match: { ...props.modelValue }, path: sample.value },
      request.signal,
    );
    if (attempt === ticket) result.value = response;
  } catch (reason) {
    if (attempt === ticket) error.value = reason;
  } finally {
    if (attempt === ticket) {
      controller = undefined;
      busy.value = false;
    }
  }
}
onUnmounted(invalidate);
</script>
<template>
  <fieldset class="matcher-input" :disabled="disabled">
    <div class="two-columns">
      <label
        >{{ t("Match type")
        }}<SelectMenu
          name="match_type"
          :model-value="modelValue.type"
          :disabled="disabled"
          :label="t('Match type')"
          :options="[
            { value: 'glob', label: t('Glob') },
            { value: 're2', label: t('RE2 regular expression') },
          ]"
          @update:model-value="changeType"
      /></label>
      <label
        >{{ t("Path pattern")
        }}<input
          ref="patternInput"
          name="match_pattern"
          :value="modelValue.pattern"
          required
          maxlength="1024"
          spellcheck="false"
          @input="
            emit('update:modelValue', {
              ...modelValue,
              pattern: ($event.target as HTMLInputElement).value,
            })
          "
      /></label>
    </div>
    <p class="muted small-text">
      {{
        t(
          "Paths start with / relative to this application, exclude query strings and are decoded once. Matching is case-sensitive.",
        )
      }}
    </p>
    <p v-if="patternTooLong" class="error" role="alert">
      {{ t("Patterns may contain at most 1,024 UTF-8 bytes.") }}
    </p>
    <p v-if="modelValue.type === 'glob'" class="muted small-text">
      {{
        t(
          "Glob matches a file or its parent directories: / matches all; /releases matches that file or subtree; /releases/ matches only the directory subtree; /releases/*/ matches immediate child-directory subtrees.",
        )
      }}
    </p>
    <p v-else class="muted small-text">
      {{
        t(
          "RE2 matches the complete path, not a substring. Matching is evaluated by the server.",
        )
      }}
    </p>
    <details class="matcher-test">
      <summary>{{ t("Test a sample path") }}</summary>
      <label
        >{{ t("Sample path")
        }}<input v-model="sample" name="sample_path" spellcheck="false"
      /></label>
      <p class="muted small-text">
        {{
          t(
            "Enter the decoded path, for example /releases/文件.zip, without URL encoding.",
          )
        }}
      </p>
      <button
        type="button"
        class="secondary"
        :disabled="busy || patternTooLong || !sample || !modelValue.pattern"
        @click="test"
      >
        {{ busy ? t("Loading…") : t("Test match") }}
      </button>
      <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
      <p v-if="result" class="notice matcher-result" role="status">
        {{ result.matches ? t("Matches") : t("Does not match") }} ·
        <code>{{ result.canonical_path }}</code>
      </p>
    </details>
  </fieldset>
</template>

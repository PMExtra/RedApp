<script setup lang="ts">
import SelectMenu from "./SelectMenu.vue";
import { computed } from "vue";
import {
  REDACTED_PROXY_PASSWORD,
  type ProxyConfig,
  type ProxyEffective,
} from "../configuration";
import { t } from "../i18n";
const props = defineProps<{
  modelValue: ProxyConfig;
  effective?: ProxyEffective;
  global?: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [ProxyConfig] }>();
const redacted = computed(() =>
  (props.modelValue.url || "").includes(`:${REDACTED_PROXY_PASSWORD}@`),
);
function mode(value: string) {
  emit(
    "update:modelValue",
    value === "url"
      ? { mode: "url", url: props.modelValue.url || "" }
      : { mode: value as "inherit" | "direct" },
  );
}
</script>
<template>
  <section class="proxy-section">
    <div v-if="!global" class="field-heading">
      <h3>{{ t("Upstream proxy") }}</h3>
      <slot name="reset" />
    </div>
    <div class="proxy-controls">
    <SelectMenu
      class="proxy-mode"
      :model-value="modelValue.mode"
      :disabled="disabled"
      :label="t('Proxy mode')"
      :options="[
        ...(global ? [] : [{ value: 'inherit', label: t('Use parent setting') }]),
        { value: 'direct', label: t('Direct connection') },
        { value: 'url', label: t('Proxy URL') },
      ]"
      @update:model-value="mode"
    /><input
      v-if="modelValue.mode === 'url'"
      class="proxy-url"
      type="text"
      :value="modelValue.url"
      :disabled="disabled"
      :aria-label="t('Proxy URL')"
      autocomplete="off"
      spellcheck="false"
      required
      @input="
        emit('update:modelValue', {
          mode: 'url',
          url: ($event.target as HTMLInputElement).value,
        })
      "
    />
    </div>
    <p
      v-if="modelValue.mode === 'url' && redacted"
      class="muted small-text redacted-password-hint"
    >
      {{
        t(
          "The saved password is hidden as ****. Keep **** to retain it for the same scheme, user and host, or enter a new password.",
        )
      }}
    </p>
    <p v-if="!global" class="muted small-text">
      {{
        t(
          "Use parent setting follows the vendor or global network setting, including direct connection; Reset restores the template field.",
        )
      }}
    </p>
    <p v-if="effective" class="muted small-text">
      {{ t("Effective proxy") }}:
      {{
        effective.mode === "direct" ? t("Direct connection") : t("Proxy URL")
      }}
      ·
      {{
        effective.source_scope === "app"
          ? t("Application")
          : effective.source_scope === "vendor"
            ? t("Vendor")
            : t("Global")
      }}
      {{ effective.source_id }} · DNS {{ effective.dns }}
    </p>
  </section>
</template>

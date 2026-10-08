<script setup lang="ts">
import SelectMenu from "./SelectMenu.vue";
import type { ProxyConfig, ProxyEffective } from "../configuration";
import { t } from "../i18n";
const props = defineProps<{
  modelValue: ProxyConfig;
  effective?: ProxyEffective;
  global?: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [ProxyConfig] }>();
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
    <h3 v-if="!global">{{ t("Upstream proxy") }}</h3>
    <SelectMenu
      :model-value="modelValue.mode"
      :disabled="disabled"
      :label="t('Proxy mode')"
      :options="[
        ...(global ? [] : [{ value: 'inherit', label: t('Use parent proxy') }]),
        { value: 'direct', label: t('Direct connection') },
        { value: 'url', label: t('Proxy URL') },
      ]"
      @update:model-value="mode"
    /><input
      v-if="modelValue.mode === 'url'"
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
    <p v-if="!global" class="muted small-text">
      {{
        t(
          "Use parent proxy follows the vendor or global network setting; Restore template restores the template field.",
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

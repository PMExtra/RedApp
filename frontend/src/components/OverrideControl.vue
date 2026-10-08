<script setup lang="ts">
import { computed } from "vue";
import { getLeaf, type Configuration } from "../configuration";
import { t } from "../i18n";
const props = defineProps<{
  configuration?: Pick<Configuration, "fields" | "defaults" | "effective" | "template_ref">;
  path: string;
  custom?: boolean;
  restored?: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ restore: []; customize: [] }>();
const inherited = computed(
  () =>
    props.restored ||
    (!props.custom &&
      props.configuration?.fields[props.path]?.source === "inherited"),
);
const comparison = computed(
  () =>
    `${t("Template")}: ${JSON.stringify(getLeaf(props.configuration?.defaults, props.path))}\n${t("Custom")}: ${JSON.stringify(getLeaf(props.configuration?.effective, props.path))}`,
);
</script>
<template>
  <span v-if="configuration" class="override-control"
    ><small :title="comparison">{{
      inherited ? t("Template") : t("Custom")
    }}</small
    ><button
      v-if="configuration.template_ref"
      type="button"
      class="secondary"
      :disabled="disabled"
      :title="comparison"
      @click="inherited ? emit('customize') : emit('restore')"
    >
      {{ inherited ? t("Customize") : t("Restore template") }}
    </button></span
  >
</template>

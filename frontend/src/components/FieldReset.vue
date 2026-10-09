<script setup lang="ts">
import { computed } from "vue";
import { getLeaf, type Configuration } from "../configuration";
import { t } from "../i18n";
// Shown only for a field with a template when it is overridden or edited in the draft.
const props = defineProps<{
  configuration?: Pick<Configuration, "fields" | "defaults" | "template_ref">;
  path: string;
  label?: string;
  modified?: boolean;
  restored?: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ reset: [] }>();
const visible = computed(
  () =>
    !!props.configuration?.template_ref &&
    !props.restored &&
    (props.modified ||
      props.configuration.fields[props.path]?.source === "custom"),
);
const title = computed(
  () =>
    `${t("Restore the template default")}: ${JSON.stringify(getLeaf(props.configuration?.defaults, props.path)) ?? ""}`,
);
</script>
<template>
  <button
    v-if="visible"
    type="button"
    class="field-reset"
    :disabled="disabled"
    :title="title"
    :aria-label="label ? `${t('Reset')}: ${label}` : t('Reset')"
    @click="emit('reset')"
  >
    {{ t("Reset") }}
  </button>
</template>

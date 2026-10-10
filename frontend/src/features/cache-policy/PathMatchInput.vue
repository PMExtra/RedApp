<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { describeError } from "@/shared/api";
import { Alert, Button, Field, Input, Select, type SelectOption } from "@/shared/ui";
import { patternTooLong, useTestPathMatch, type PathMatch } from "./queries";

/**
 * A glob (doublestar) or RE2 path pattern with an optional server-side test
 * against a sample path (`testPathMatch`).
 */
const model = defineModel<PathMatch>({ required: true });
const props = withDefaults(
  defineProps<{
    disabled?: boolean;
    /** Group label, e.g. "Rule 2 pattern". */
    legend?: string;
    /** Show the sample-path tester. */
    testable?: boolean;
  }>(),
  { testable: true },
);
const { t } = useI18n();
const sample = ref("/");
const test = useTestPathMatch();

const typeOptions = computed<SelectOption[]>(() => [
  { value: "glob", label: t("cachePolicy.match.glob") },
  { value: "re2", label: t("cachePolicy.match.re2") },
]);
const type = computed({
  get: () => model.value.type,
  set: (value: string | undefined) => {
    if (value === "glob" || value === "re2") model.value = { ...model.value, type: value };
  },
});
const pattern = computed({
  get: () => model.value.pattern,
  set: (value: string) => {
    model.value = { ...model.value, pattern: value };
  },
});
const tooLong = computed(() => patternTooLong(model.value.pattern));
const patternError = computed(() => (tooLong.value ? t("cachePolicy.match.tooLong") : undefined));

// A result belongs to the pattern and sample it was computed for.
watch([() => model.value.type, () => model.value.pattern, sample], () => {
  test.reset();
});

const testError = computed(() => (test.error.value ? describeError(test.error.value) : undefined));

function run() {
  if (!model.value.pattern || !sample.value || tooLong.value) return;
  test.mutate({ match: { ...model.value }, path: sample.value });
}
</script>

<template>
  <fieldset class="flex min-w-0 flex-col gap-3" :disabled="props.disabled">
    <legend v-if="legend" class="sr-only">{{ legend }}</legend>
    <div class="grid gap-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
      <Field v-slot="{ control }" :label="t('cachePolicy.match.type')">
        <Select v-bind="control" v-model="type" :options="typeOptions" :disabled="disabled" />
      </Field>
      <Field
        v-slot="{ control }"
        :label="t('cachePolicy.match.pattern')"
        :description="
          type === 'glob' ? t('cachePolicy.match.globHint') : t('cachePolicy.match.re2Hint')
        "
        :error="patternError"
        required
      >
        <Input
          v-bind="control"
          v-model="pattern"
          class="font-mono"
          spellcheck="false"
          autocomplete="off"
          :disabled="disabled"
        />
      </Field>
    </div>
    <details v-if="testable" class="group">
      <summary
        class="inline-flex cursor-pointer items-center gap-1 rounded-sm text-sm font-medium focus-ring"
      >
        <ChevronRight class="size-4 transition-transform group-open:rotate-90" aria-hidden="true" />
        {{ t("cachePolicy.match.testTitle") }}
      </summary>
      <div class="mt-2 flex flex-col gap-2">
        <div class="flex flex-wrap items-start gap-2">
          <Field
            v-slot="{ control }"
            class="min-w-0 flex-1"
            :label="t('cachePolicy.match.sample')"
            :description="t('cachePolicy.match.sampleHint')"
          >
            <Input
              v-bind="control"
              v-model="sample"
              class="font-mono"
              spellcheck="false"
              autocomplete="off"
              maxlength="4096"
            />
          </Field>
          <Button
            class="mt-6.5"
            :loading="test.isPending.value"
            :disabled="disabled || !pattern || !sample || tooLong"
            @click="run"
          >
            {{ t("cachePolicy.match.test") }}
          </Button>
        </div>
        <Alert v-if="test.data.value" :tone="test.data.value.matches ? 'success' : 'info'">
          {{
            test.data.value.matches
              ? t("cachePolicy.match.matches", { path: test.data.value.path })
              : t("cachePolicy.match.noMatch", { path: test.data.value.path })
          }}
        </Alert>
        <Alert v-else-if="testError" tone="danger">
          {{ testError.message }}
          <span v-if="testError.detail" class="block text-xs text-muted">
            {{ testError.detail }}
          </span>
        </Alert>
      </div>
    </details>
  </fieldset>
</template>

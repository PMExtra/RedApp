<script setup lang="ts">
import { computed, ref } from "vue";
import { Plus, Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import {
  FieldReset,
  useAppConfiguration,
  useAppConfigurationPatch,
  useOverlayDraft,
} from "@/features/configuration";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import {
  Alert,
  AsyncState,
  Button,
  Card,
  IconButton,
  RadioGroup,
  RevisionConflictAlert,
  SortableList,
  Switch,
  type RadioOption,
} from "@/shared/ui";
import DurationInput from "./DurationInput.vue";
import PathMatchInput from "./PathMatchInput.vue";
import { patternTooLong, type CacheRule, type CleanupRule } from "./queries";

/**
 * HTTP cache policy of an `http-cache` application: stale fallback, path TTL
 * rules and automatic cleanup rules (`http_policy.*` of the configuration).
 * Rendered by the application settings page.
 */
const props = defineProps<{ vendor: string; app: string; readOnly?: boolean }>();
const { t } = useI18n();
const MAX_RULES = 32;

interface Values {
  "http_policy.stale_fallback": boolean;
  "http_policy.rules": CacheRule[];
  "http_policy.auto_cleanup": CleanupRule[];
}

const configuration = useAppConfiguration(
  () => props.vendor,
  () => props.app,
);
const overlay = useOverlayDraft<Values>(configuration.data, (spec) =>
  spec?.http_policy
    ? {
        "http_policy.stale_fallback": spec.http_policy.stale_fallback,
        "http_policy.rules": spec.http_policy.rules,
        "http_policy.auto_cleanup": spec.http_policy.auto_cleanup,
      }
    : undefined,
);
const { draft } = overlay;
const save = useAppConfigurationPatch(
  () => props.vendor,
  () => props.app,
  configuration.data,
);
const submitted = ref(false);
const linked = computed(() => (configuration.data.value?.template_ref ?? null) !== null);
const fields = computed(() => configuration.data.value?.fields ?? {});
const busy = computed(() => save.isPending.value);
useDirtyGuard(overlay.dirty);

// Stable keys for the reorderable rows (rule objects keep their identity while edited).
const keys = new WeakMap<object, string>();
let nextKey = 0;
function keyOf(rule: object): string {
  let key = keys.get(rule);
  if (!key) {
    key = `rule-${String(nextKey++)}`;
    keys.set(rule, key);
  }
  return key;
}

const basisOptions = computed<RadioOption[]>(() => [
  {
    value: "last_access",
    label: t("cachePolicy.basis.last_access"),
    description: t("cachePolicy.basis.lastAccessHint"),
  },
  {
    value: "fetched_at",
    label: t("cachePolicy.basis.fetched_at"),
    description: t("cachePolicy.basis.fetchedAtHint"),
  },
]);

function patternError(pattern: string): string | undefined {
  if (!pattern) return submitted.value ? t("cachePolicy.match.required") : undefined;
  return patternTooLong(pattern) ? t("cachePolicy.match.tooLong") : undefined;
}

const invalid = computed(() => {
  const value = draft.value;
  if (!value) return true;
  const badMatch = (rule: { match: { pattern: string } }) =>
    !rule.match.pattern || patternTooLong(rule.match.pattern);
  return (
    value["http_policy.rules"].some(
      (rule) => badMatch(rule) || !Number.isInteger(rule.ttl_seconds),
    ) ||
    value["http_policy.auto_cleanup"].some(
      (rule) => badMatch(rule) || !Number.isInteger(rule.age_seconds) || rule.age_seconds < 60,
    )
  );
});

function ruleLabel(key: string, list: readonly object[] | undefined, rule: object): string {
  return t(key, { number: (list?.indexOf(rule) ?? 0) + 1 });
}

function addRule() {
  draft.value?.["http_policy.rules"].push({
    match: { type: "glob", pattern: "/" },
    ttl_seconds: 300,
  });
}

function addCleanupRule() {
  draft.value?.["http_policy.auto_cleanup"].push({
    match: { type: "glob", pattern: "/" },
    basis: "last_access",
    age_seconds: 30 * 86_400,
  });
}

function setBasis(rule: CleanupRule, value: string | undefined) {
  if (value === "last_access" || value === "fetched_at") rule.basis = value;
}

function submit() {
  submitted.value = true;
  const body = overlay.patch.value;
  if (!body || invalid.value || props.readOnly) return;
  save.mutate(body, {
    onSuccess: () => {
      overlay.reset();
      submitted.value = false;
      toast({ tone: "success", title: t("cachePolicy.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  overlay.reset();
}
</script>

<template>
  <Card :title="t('cachePolicy.title')" :description="t('cachePolicy.description')">
    <AsyncState
      :loading="configuration.isPending.value"
      :error="configuration.error.value"
      @retry="configuration.refetch()"
    >
      <form v-if="draft" class="flex flex-col gap-6" novalidate @submit.prevent="submit">
        <Alert v-if="configuration.data.value?.template_missing" tone="warning">
          {{ t("cachePolicy.templateMissing") }}
        </Alert>
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />

        <fieldset :disabled="readOnly || busy" class="flex flex-col gap-6">
          <section class="flex items-start gap-3">
            <Switch
              id="cache-policy-stale-fallback"
              v-model="draft['http_policy.stale_fallback']"
              aria-describedby="cache-policy-stale-fallback-hint"
            />
            <div class="flex min-w-0 flex-1 flex-col gap-1">
              <label for="cache-policy-stale-fallback" class="text-sm font-medium">
                {{ t("cachePolicy.staleFallback") }}
              </label>
              <p id="cache-policy-stale-fallback-hint" class="text-xs text-muted">
                {{ t("cachePolicy.staleFallbackHint") }}
              </p>
            </div>
            <FieldReset
              :linked="linked"
              :origin="fields['http_policy.stale_fallback']"
              :modified="overlay.modified('http_policy.stale_fallback')"
              @reset="overlay.restore('http_policy.stale_fallback')"
            />
          </section>

          <section class="flex flex-col gap-3" aria-labelledby="cache-policy-rules">
            <div class="flex items-start justify-between gap-3">
              <div class="flex flex-col gap-1">
                <h3 id="cache-policy-rules" class="text-sm font-semibold">
                  {{ t("cachePolicy.rules.title") }}
                </h3>
                <p class="text-xs text-muted">{{ t("cachePolicy.rules.hint") }}</p>
              </div>
              <FieldReset
                :linked="linked"
                :origin="fields['http_policy.rules']"
                :modified="overlay.modified('http_policy.rules')"
                @reset="overlay.restore('http_policy.rules')"
              />
            </div>
            <SortableList
              v-if="draft['http_policy.rules'].length"
              v-model="draft['http_policy.rules']"
              :item-key="keyOf"
              :item-label="
                (rule) => ruleLabel('cachePolicy.rules.rule', draft?.['http_policy.rules'], rule)
              "
              :disabled="readOnly || busy"
            >
              <template #item="{ item: rule, index }">
                <div class="flex flex-col gap-3 py-1">
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-sm font-medium">
                      {{ t("cachePolicy.rules.rule", { number: index + 1 }) }}
                    </span>
                    <IconButton
                      size="sm"
                      :label="t('cachePolicy.rules.remove', { number: index + 1 })"
                      @click="draft['http_policy.rules'].splice(index, 1)"
                    >
                      <Trash2 aria-hidden="true" />
                    </IconButton>
                  </div>
                  <PathMatchInput
                    v-model="rule.match"
                    :legend="t('cachePolicy.rules.rule', { number: index + 1 })"
                    :disabled="readOnly || busy"
                  />
                  <p v-if="patternError(rule.match.pattern)" class="text-xs text-danger">
                    {{ patternError(rule.match.pattern) }}
                  </p>
                  <DurationInput
                    v-model="rule.ttl_seconds"
                    :label="t('cachePolicy.rules.ttl')"
                    :description="t('cachePolicy.rules.ttlHint')"
                    :min="0"
                    :max="86400"
                    :disabled="readOnly || busy"
                    required
                  />
                </div>
              </template>
            </SortableList>
            <p v-else class="text-sm text-muted">{{ t("cachePolicy.rules.empty") }}</p>
            <div>
              <Button
                size="sm"
                :disabled="draft['http_policy.rules'].length >= MAX_RULES"
                @click="addRule"
              >
                <Plus aria-hidden="true" />
                {{ t("cachePolicy.rules.add") }}
              </Button>
            </div>
          </section>

          <section class="flex flex-col gap-3" aria-labelledby="cache-policy-cleanup">
            <div class="flex items-start justify-between gap-3">
              <div class="flex flex-col gap-1">
                <h3 id="cache-policy-cleanup" class="text-sm font-semibold">
                  {{ t("cachePolicy.cleanup.title") }}
                </h3>
                <p class="text-xs text-muted">{{ t("cachePolicy.cleanup.hint") }}</p>
              </div>
              <FieldReset
                :linked="linked"
                :origin="fields['http_policy.auto_cleanup']"
                :modified="overlay.modified('http_policy.auto_cleanup')"
                @reset="overlay.restore('http_policy.auto_cleanup')"
              />
            </div>
            <SortableList
              v-if="draft['http_policy.auto_cleanup'].length"
              v-model="draft['http_policy.auto_cleanup']"
              :item-key="keyOf"
              :item-label="
                (rule) =>
                  ruleLabel('cachePolicy.cleanup.rule', draft?.['http_policy.auto_cleanup'], rule)
              "
              :disabled="readOnly || busy"
            >
              <template #item="{ item: rule, index }">
                <div class="flex flex-col gap-3 py-1">
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-sm font-medium">
                      {{ t("cachePolicy.cleanup.rule", { number: index + 1 }) }}
                    </span>
                    <IconButton
                      size="sm"
                      :label="t('cachePolicy.cleanup.remove', { number: index + 1 })"
                      @click="draft['http_policy.auto_cleanup'].splice(index, 1)"
                    >
                      <Trash2 aria-hidden="true" />
                    </IconButton>
                  </div>
                  <PathMatchInput
                    v-model="rule.match"
                    :legend="t('cachePolicy.cleanup.rule', { number: index + 1 })"
                    :disabled="readOnly || busy"
                  />
                  <p v-if="patternError(rule.match.pattern)" class="text-xs text-danger">
                    {{ patternError(rule.match.pattern) }}
                  </p>
                  <fieldset class="flex flex-col gap-2">
                    <legend class="mb-1 text-sm font-medium">
                      {{ t("cachePolicy.basis.label") }}
                    </legend>
                    <RadioGroup
                      :model-value="rule.basis"
                      :options="basisOptions"
                      @update:model-value="setBasis(rule, $event)"
                    />
                  </fieldset>
                  <DurationInput
                    v-model="rule.age_seconds"
                    :label="t('cachePolicy.cleanup.age')"
                    :description="t('cachePolicy.cleanup.ageHint')"
                    :min="60"
                    :max="315360000"
                    :disabled="readOnly || busy"
                    required
                  />
                </div>
              </template>
            </SortableList>
            <Alert v-else tone="info">{{ t("cachePolicy.cleanup.empty") }}</Alert>
            <div>
              <Button
                size="sm"
                :disabled="draft['http_policy.auto_cleanup'].length >= MAX_RULES"
                @click="addCleanupRule"
              >
                <Plus aria-hidden="true" />
                {{ t("cachePolicy.cleanup.add") }}
              </Button>
            </div>
          </section>
        </fieldset>

        <p class="text-xs text-muted">{{ t("cachePolicy.limit", { max: MAX_RULES }) }}</p>
        <div v-if="!readOnly" class="flex flex-wrap gap-2">
          <Button
            type="submit"
            variant="primary"
            :loading="busy"
            :disabled="!overlay.dirty.value || (submitted && invalid)"
          >
            {{ t("cachePolicy.save") }}
          </Button>
          <Button :disabled="!overlay.dirty.value || busy" @click="overlay.reset()">
            {{ t("cachePolicy.discard") }}
          </Button>
        </div>
      </form>
    </AsyncState>
  </Card>
</template>

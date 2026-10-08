<script setup lang="ts">
import IconButton from "./IconButton.vue";
import SortableList from "./SortableList.vue";
import SelectMenu from "./SelectMenu.vue";
import SwitchControl from "./SwitchControl.vue";
import { computed } from "vue";
import type { CachePolicy } from "../cachePolicy";
import { useConfiguration } from "../composables/useConfiguration";
import OverrideControl from "./OverrideControl.vue";
import { errorText, t } from "../i18n";
import MatcherInput from "./MatcherInput.vue";
import DurationInput from "./DurationInput.vue";
const props = defineProps<{ application: string }>();
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
} = useConfiguration<CachePolicy>(
  computed(() => `apps/${props.application}/configuration`),
  "http_policy",
  ["rules", "auto_cleanup", "stale_fallback"],
);
const busy = computed(() => loading.value || saving.value);
type RuleList = "rules" | "auto_cleanup";
function add(kind: RuleList) {
  if (!draft.value || draft.value[kind].length >= 32) return;
  mark(kind);
  const match = { type: "glob" as const, pattern: "/" };
  if (kind === "rules") draft.value.rules.push({ match, ttl_seconds: 300 });
  else
    draft.value.auto_cleanup.push({
      match,
      basis: "last_access",
      age_seconds: 30 * 86400,
    });
}
</script>
<template>
  <section class="panel cache-policy">
    <h2>{{ t("Cache rules") }}</h2>
    <p v-if="error" class="error policy-error" role="alert">
      {{ errorText(error) }}
    </p>
    <p v-if="saved" class="notice" role="status">
      {{ t("Cache rules saved.") }}
    </p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form @submit.prevent="save">
      <fieldset v-if="draft" :disabled="busy">
        <SwitchControl
          v-model="draft.stale_fallback"
          @update:model-value="mark('stale_fallback')"
          name="stale_fallback"
          :label="t('Use stale cache on origin failure')"
          :disabled="busy"
        />
        <p class="muted small-text">
          {{
            t(
              "Enabled by default for all paths, including TTL 0. Disabling fallback returns an error on origin failure and keeps stored files.",
            )
          }}
        </p>
        <OverrideControl
          :configuration="configuration"
          path="http_policy.stale_fallback"
          :custom="touched.has('stale_fallback')"
          :restored="unsets.has('stale_fallback')"
          @restore="restore('stale_fallback')"
          @customize="mark('stale_fallback')"
        />
        <section class="ttl-rules" @input="mark('rules')">
          <h3>{{ t("Path TTL rules") }}</h3>
          <OverrideControl
            :configuration="configuration"
            path="http_policy.rules"
            :custom="touched.has('rules')"
            :restored="unsets.has('rules')"
            @restore="restore('rules')"
            @customize="mark('rules')"
          />
          <p class="muted">
            {{
              t(
                "Rules run from top to bottom. The first matching path sets its TTL. Otherwise Cache-Control takes priority; the application default TTL applies only when Cache-Control is absent.",
              )
            }}
          </p>
          <p class="muted">
            {{
              t(
                "TTL 0 checks the origin on every request and retains a complete copy. The stale fallback setting controls reuse on failure. Positive TTL rules can cache responses marked no-store or private by the origin.",
              )
            }}
          </p>
          <SortableList
            v-model="draft.rules"
            @update:model-value="mark('rules')"
            :label="t('Path TTL rules')"
            :item-label="
              (_rule, index) => t('Rule {number}', { number: index + 1 })
            "
            :disabled="busy"
            row-class="policy-rule"
          >
            <template #default="{ item: rule, index }">
              <div class="rule-heading">
                <strong>{{ t("Rule {number}", { number: index + 1 }) }}</strong>
                <IconButton
                  icon="close"
                  class="secondary"
                  :label="`${t('Remove rule')}: ${index + 1}`"
                  @click="
                    draft.rules.splice(index, 1);
                    mark('rules');
                  "
                />
              </div>
              <MatcherInput
                v-model="rule.match"
                @update:model-value="mark('rules')"
                :application="application"
                :disabled="busy"
              />
              <label
                >{{ t("TTL (seconds)")
                }}<input
                  v-model.number="rule.ttl_seconds"
                  name="rule_ttl"
                  type="number"
                  min="0"
                  max="86400"
                  step="1"
                  required
              /></label>
            </template>
          </SortableList>
          <p v-if="!draft.rules.length" class="muted">
            {{
              t(
                "Cache-Control determines freshness; the application default TTL applies only when that header is absent.",
              )
            }}
          </p>
          <IconButton
            type="button"
            class="secondary"
            :disabled="draft.rules.length >= 32"
            @click="add('rules')"
            icon="plus"
            :label="t('Add TTL rule')"
          />
        </section>
        <section class="auto-cleanup-rules" @input="mark('auto_cleanup')">
          <h3>{{ t("Automatic cleanup rules") }}</h3>
          <OverrideControl
            :configuration="configuration"
            path="http_policy.auto_cleanup"
            :custom="touched.has('auto_cleanup')"
            :restored="unsets.has('auto_cleanup')"
            @restore="restore('auto_cleanup')"
            @customize="mark('auto_cleanup')"
          />
          <p class="muted">
            {{
              t(
                "The first matching path rule owns the file. If its age is not reached, later rules do not apply.",
              )
            }}
          </p>
          <p class="muted">
            {{
              t(
                "Saved rules run every 15 minutes for current active sources only. Each application pass scans at most 1,000 files and retires at most 100.",
              )
            }}
          </p>
          <SortableList
            v-model="draft.auto_cleanup"
            @update:model-value="mark('auto_cleanup')"
            :label="t('Automatic cleanup rules')"
            :item-label="
              (_rule, index) => t('Rule {number}', { number: index + 1 })
            "
            :disabled="busy"
            row-class="policy-rule"
          >
            <template #default="{ item: rule, index }">
              <div class="rule-heading">
                <strong>{{ t("Rule {number}", { number: index + 1 }) }}</strong>
                <IconButton
                  icon="close"
                  class="secondary"
                  :label="`${t('Remove rule')}: ${index + 1}`"
                  @click="
                    draft.auto_cleanup.splice(index, 1);
                    mark('auto_cleanup');
                  "
                />
              </div>
              <MatcherInput
                v-model="rule.match"
                @update:model-value="mark('auto_cleanup')"
                :application="application"
                :disabled="busy"
              />
              <label
                >{{ t("Select files by")
                }}<SelectMenu
                  v-model="rule.basis"
                  @update:model-value="mark('auto_cleanup')"
                  name="rule_basis"
                  :label="t('Select files by')"
                  :options="[
                    { value: 'fetched_at', label: t('Fetched at') },
                    { value: 'last_access', label: t('Last accessed') },
                  ]"
              /></label>
              <p class="muted small-text">
                {{
                  rule.basis === "fetched_at"
                    ? t(
                        "Fetched-time cleanup can retire files that are still frequently accessed.",
                      )
                    : t(
                        "Files accessed during cleanup are checked again and retained.",
                      )
                }}
              </p>
              <DurationInput
                v-model="rule.age_seconds"
                @update:model-value="mark('auto_cleanup')"
              />
            </template>
          </SortableList>
          <p v-if="!draft.auto_cleanup.length" class="notice">
            {{
              t(
                "Automatic cleanup is disabled until rules are added and saved.",
              )
            }}
          </p>
          <IconButton
            type="button"
            class="secondary"
            :disabled="draft.auto_cleanup.length >= 32"
            @click="add('auto_cleanup')"
            icon="plus"
            :label="t('Add automatic cleanup rule')"
          />
        </section>
        <p class="muted small-text">
          {{
            t("Up to 32 rules per list. Save explicitly to apply these rules.")
          }}
        </p>
      </fieldset>
      <div class="form-actions">
        <button :disabled="busy || !draft">
          {{ saving ? t("Saving…") : t("Save cache rules") }}</button
        ><IconButton
          type="button"
          class="secondary"
          :disabled="busy"
          @click="load()"
          icon="refresh"
          :label="t('Reload')"
        />
      </div>
    </form>
  </section>
</template>

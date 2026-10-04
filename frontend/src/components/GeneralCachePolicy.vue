<script setup lang="ts">
import SelectMenu from "./SelectMenu.vue";
import SwitchControl from "./SwitchControl.vue";
import { computed } from "vue";
import { appAPI } from "../bootstrap";
import { bytes } from "../api";
import type { CachePolicy } from "../cachePolicy";
import { useSetting } from "../composables/useSetting";
import { errorText, localDate, t } from "../i18n";
import MatcherInput from "./MatcherInput.vue";
import DurationInput from "./DurationInput.vue";
const props = defineProps<{ application: string }>();
const { draft, loading, saving, error, saved, load, save } =
  useSetting<CachePolicy>(
    computed(() => `${appAPI(props.application)}/cache/policy`),
  );
const busy = computed(() => loading.value || saving.value);
type RuleList = "rules" | "auto_cleanup";
function move(kind: RuleList, index: number, delta: number) {
  const rules = draft.value?.[kind];
  if (!rules || index + delta < 0 || index + delta >= rules.length) return;
  [rules[index], rules[index + delta]] = [rules[index + delta]!, rules[index]!];
}
function add(kind: RuleList) {
  if (!draft.value || draft.value[kind].length >= 32) return;
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
    <form @submit.prevent="save">
      <fieldset v-if="draft" :disabled="busy">
        <SwitchControl
          v-model="draft.stale_fallback"
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
        <section class="ttl-rules">
          <h3>{{ t("Path TTL rules") }}</h3>
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
          <div
            v-for="(rule, index) in draft.rules"
            :key="rule.id || index"
            class="policy-rule"
          >
            <div class="rule-heading">
              <strong>{{ t("Rule {number}", { number: index + 1 }) }}</strong>
              <div class="form-actions">
                <button
                  type="button"
                  class="secondary"
                  :disabled="index === 0"
                  @click="move('rules', index, -1)"
                >
                  {{ t("Move up") }}</button
                ><button
                  type="button"
                  class="secondary"
                  :disabled="index === draft.rules.length - 1"
                  @click="move('rules', index, 1)"
                >
                  {{ t("Move down") }}</button
                ><button
                  type="button"
                  class="secondary"
                  @click="draft.rules.splice(index, 1)"
                >
                  {{ t("Remove rule") }}
                </button>
              </div>
            </div>
            <MatcherInput
              v-model="rule.match"
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
          </div>
          <p v-if="!draft.rules.length" class="muted">
            {{
              t(
                "Cache-Control determines freshness; the application default TTL applies only when that header is absent.",
              )
            }}
          </p>
          <button
            type="button"
            class="secondary"
            :disabled="draft.rules.length >= 32"
            @click="add('rules')"
          >
            {{ t("Add TTL rule") }}
          </button>
        </section>
        <section class="auto-cleanup-rules">
          <h3>{{ t("Automatic cleanup rules") }}</h3>
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
          <div
            v-for="(rule, index) in draft.auto_cleanup"
            :key="index"
            class="policy-rule"
          >
            <div class="rule-heading">
              <strong>{{ t("Rule {number}", { number: index + 1 }) }}</strong>
              <div class="form-actions">
                <button
                  type="button"
                  class="secondary"
                  :disabled="index === 0"
                  @click="move('auto_cleanup', index, -1)"
                >
                  {{ t("Move up") }}</button
                ><button
                  type="button"
                  class="secondary"
                  :disabled="index === draft.auto_cleanup.length - 1"
                  @click="move('auto_cleanup', index, 1)"
                >
                  {{ t("Move down") }}</button
                ><button
                  type="button"
                  class="secondary"
                  @click="draft.auto_cleanup.splice(index, 1)"
                >
                  {{ t("Remove rule") }}
                </button>
              </div>
            </div>
            <MatcherInput
              v-model="rule.match"
              :application="application"
              :disabled="busy"
            />
            <label
              >{{ t("Select files by")
              }}<SelectMenu
                v-model="rule.basis"
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
            <DurationInput v-model="rule.age_seconds" />
          </div>
          <p v-if="!draft.auto_cleanup.length" class="notice">
            {{
              t(
                "Automatic cleanup is disabled until rules are added and saved.",
              )
            }}
          </p>
          <button
            type="button"
            class="secondary"
            :disabled="draft.auto_cleanup.length >= 32"
            @click="add('auto_cleanup')"
          >
            {{ t("Add automatic cleanup rule") }}
          </button>
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
        ><button
          type="button"
          class="secondary"
          :disabled="busy"
          @click="load()"
        >
          {{ t("Reload") }}
        </button>
      </div>
    </form>
  </section>
</template>

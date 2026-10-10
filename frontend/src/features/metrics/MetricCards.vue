<script setup lang="ts">
import { computed } from "vue";
import { ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { Badge, Card } from "@/shared/ui";
import {
  GLOBAL_COMMON_METRICS,
  METRIC_GROUPS,
  useMetricLabels,
  type Metric,
  type MetricKey,
} from "./catalog";
import { useMetricFormat } from "./format";
import type { MetricScope } from "./queries";

/**
 * Metric values grouped by the spec's group enum. `primary` keys are shown
 * as common metrics; the rest are diagnostic and start collapsed. Every
 * value is a button that emits `select` (open its history).
 */
const props = withDefaults(
  defineProps<{
    metrics: Metric[];
    /** Keys of the common metrics, in any order (catalog order is kept). */
    primary?: readonly MetricKey[];
    scope?: MetricScope;
    /** Heading level of the two section titles. */
    level?: 2 | 3;
  }>(),
  { primary: () => GLOBAL_COMMON_METRICS, scope: () => ({ kind: "global" }), level: 2 },
);
const emit = defineEmits<{ select: [metric: Metric] }>();
const { t } = useI18n();
const labels = useMetricLabels();
const format = useMetricFormat();

function grouped(items: Metric[]) {
  return METRIC_GROUPS.map((group) => ({
    group,
    items: items.filter((metric) => metric.group === group),
  })).filter((entry) => entry.items.length > 0);
}

const primaryKeys = computed(() => new Set<string>(props.primary));
const common = computed(() =>
  grouped(props.metrics.filter((metric) => primaryKeys.value.has(metric.key))),
);
const diagnostic = computed(() =>
  grouped(props.metrics.filter((metric) => !primaryKeys.value.has(metric.key))),
);
const diagnosticCount = computed(() =>
  diagnostic.value.reduce((sum, entry) => sum + entry.items.length, 0),
);
const commonCount = computed(() =>
  common.value.reduce((sum, entry) => sum + entry.items.length, 0),
);
const subLevel = computed(() => (props.level === 2 ? "h3" : "h4"));

function hint(metric: Metric) {
  if (metric.key === "versions.total") {
    return t(
      props.scope.kind === "app" ? "metrics.versionsScope.app" : "metrics.versionsScope.global",
    );
  }
  return t(`metrics.kinds.${metric.kind}`);
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card
      v-if="commonCount > 0"
      :level="level"
      :title="t('metrics.sections.common')"
      :description="t('metrics.sections.commonDescription')"
    >
      <div class="flex flex-col gap-5">
        <section v-for="entry in common" :key="entry.group" class="flex flex-col gap-2">
          <component
            :is="subLevel"
            class="text-xs font-semibold tracking-wide text-muted uppercase"
          >
            {{ labels.group(entry.group) }}
          </component>
          <ul class="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
            <li v-for="metric in entry.items" :key="metric.key">
              <button
                type="button"
                class="group flex h-full w-full flex-col gap-1 rounded-lg border border-border bg-surface p-3 text-start hover:border-border-strong hover:bg-surface-hover focus-ring"
                :aria-label="
                  t('metrics.viewHistory', {
                    name: labels.label(metric),
                    value: format.value(metric.value, metric.unit),
                  })
                "
                @click="emit('select', metric)"
              >
                <span class="flex items-center justify-between gap-2 text-sm text-muted">
                  {{ labels.label(metric) }}
                  <ChevronRight
                    class="size-4 shrink-0 opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100"
                    aria-hidden="true"
                  />
                </span>
                <span class="text-xl font-semibold tabular-nums">
                  {{ format.value(metric.value, metric.unit) }}
                </span>
                <span class="text-xs text-subtle">{{ hint(metric) }}</span>
              </button>
            </li>
          </ul>
        </section>
      </div>
    </Card>

    <details
      v-if="diagnosticCount > 0"
      class="group/diag rounded-xl border border-border bg-surface shadow-sm"
    >
      <summary
        class="flex cursor-pointer list-none items-center gap-2 rounded-xl px-5 py-4 focus-ring"
      >
        <ChevronRight
          class="size-4 text-muted transition-transform group-open/diag:rotate-90"
          aria-hidden="true"
        />
        <component :is="`h${level}`" class="text-base font-semibold">
          {{ t("metrics.sections.diagnostic") }}
        </component>
        <Badge>{{ t("metrics.count", { count: diagnosticCount }) }}</Badge>
      </summary>
      <div class="flex flex-col gap-5 border-t border-border px-5 py-4">
        <p class="text-sm text-muted">{{ t("metrics.sections.diagnosticDescription") }}</p>
        <section v-for="entry in diagnostic" :key="entry.group" class="flex flex-col gap-2">
          <component
            :is="subLevel"
            class="text-xs font-semibold tracking-wide text-muted uppercase"
          >
            {{ labels.group(entry.group) }}
          </component>
          <ul class="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-2">
            <li v-for="metric in entry.items" :key="metric.key">
              <button
                type="button"
                class="flex w-full items-baseline justify-between gap-3 rounded-md border border-border px-3 py-2 text-start text-sm hover:bg-surface-hover focus-ring"
                :aria-label="
                  t('metrics.viewHistory', {
                    name: labels.label(metric),
                    value: format.value(metric.value, metric.unit),
                  })
                "
                @click="emit('select', metric)"
              >
                <span class="text-muted">{{ labels.label(metric) }}</span>
                <span class="font-medium tabular-nums">
                  {{ format.value(metric.value, metric.unit) }}
                </span>
              </button>
            </li>
          </ul>
        </section>
      </div>
    </details>
  </div>
</template>

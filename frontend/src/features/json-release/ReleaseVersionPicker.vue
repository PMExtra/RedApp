<script setup lang="ts">
import { computed } from "vue";
import { Check, ChevronDown } from "@lucide/vue";
import { DropdownMenuItemIndicator, DropdownMenuRadioGroup, DropdownMenuRadioItem } from "reka-ui";
import { I18nT, useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { Badge, cn, DropdownMenu, itemClass, RelativeTime } from "@/shared/ui";
import { useReleaseDownloads, useReleaseHistory } from "./queries";

/**
 * The version badge of a JSON release, with an arrow that lists every
 * discovered release. The model is the chosen version; `undefined` is the
 * default channel's release, which the badge shows initially.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  latest: Schema<"PublicApp">["latest_known_version"];
}>();
const model = defineModel<string | undefined>();
const { t } = useI18n();
const format = useFormat();
// Shared with the download button: the default release and its staleness.
// Reading it may refresh the channel, so the history waits for it, and its
// version is newer than the page's when the page loaded first.
const current = useReleaseDownloads(
  () => props.vendor,
  () => props.app,
);
const history = useReleaseHistory(
  () => props.vendor,
  () => props.app,
  () => !current.isPending.value,
);

const items = computed(() => history.data.value?.items ?? []);
const shown = computed(() =>
  items.value.find((item) => item.version === (model.value ?? current.data.value?.version)),
);
const version = computed(() => model.value ?? current.data.value?.version ?? props.latest?.version);
const firstSeen = computed(() => shown.value?.discovered_at ?? props.latest?.first_seen);
const stale = computed(() => !model.value && !!current.data.value?.stale);

function choose(value: unknown) {
  if (typeof value !== "string") return;
  model.value = value === current.data.value?.version ? undefined : value;
}
</script>

<template>
  <span v-if="version" class="inline-flex flex-wrap items-center gap-2">
    <DropdownMenu align="start" content-class="max-h-80 overflow-y-auto">
      <template #trigger>
        <button
          type="button"
          class="inline-flex items-center gap-1 rounded-full border border-primary/20 bg-primary-soft px-2 py-0.5 font-mono text-xs font-medium text-primary hover:bg-primary/15 focus-ring"
          :aria-label="t('releaseVersions.choose', { version })"
        >
          {{ version }}
          <ChevronDown class="size-3" aria-hidden="true" />
        </button>
      </template>
      <p v-if="history.isPending.value" class="px-2 py-1.5 text-muted">
        {{ t("releaseVersions.loading") }}
      </p>
      <p v-else-if="history.isError.value" class="px-2 py-1.5 text-muted">
        {{ t("releaseVersions.unavailable") }}
      </p>
      <DropdownMenuRadioGroup
        v-else
        :model-value="model ?? current.data.value?.version"
        @update:model-value="choose"
      >
        <DropdownMenuRadioItem
          v-for="item in items"
          :key="item.version"
          :value="item.version"
          :class="cn(itemClass, 'ps-7')"
        >
          <DropdownMenuItemIndicator class="absolute start-2 inline-flex">
            <Check class="size-4 text-primary" aria-hidden="true" />
          </DropdownMenuItemIndicator>
          <span class="font-mono">{{ item.version }}</span>
          <Badge v-for="channel in item.channels" :key="channel">{{ channel }}</Badge>
          <span class="ms-auto ps-4 text-xs text-muted">
            {{ format.relativeTime(item.discovered_at) }}
          </span>
        </DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
      <p v-if="history.data.value?.truncated" class="px-2 py-1.5 text-xs text-muted">
        {{ t("releaseVersions.truncated") }}
      </p>
    </DropdownMenu>
    <span v-if="firstSeen" class="text-xs text-muted">
      <I18nT keypath="catalog.app.firstSeen" scope="global">
        <template #time>
          <RelativeTime :value="firstSeen" />
        </template>
      </I18nT>
    </span>
    <Badge v-if="stale" tone="warning">{{ t("releaseDownloads.stale") }}</Badge>
  </span>
  <span v-else class="text-sm text-muted">{{ t("catalog.app.versionUnknown") }}</span>
</template>

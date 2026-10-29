<script setup lang="ts">
import { computed } from "vue";
import { Check, ChevronDown, Download } from "@lucide/vue";
import { DropdownMenuLabel, DropdownMenuSeparator } from "reka-ui";
import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import { Button, DropdownMenu, DropdownMenuItem, Skeleton } from "@/shared/ui";
import { useEnvironment } from "./environment";
import { useReleaseDownloads } from "./queries";
import { recommend, type Artifact } from "./recommendation";

/**
 * Split download button of a JSON release: the main part downloads the
 * package recommended for the visitor's environment, the arrow lists every
 * other visible package of the same release. `release` selects a discovered
 * release; without it, the default channel's.
 */
const props = defineProps<{ vendor: string; app: string; release?: string }>();
const { t } = useI18n();
const format = useFormat();
const query = useReleaseDownloads(
  () => props.vendor,
  () => props.app,
  () => props.release,
);
const environment = useEnvironment();

const visible = computed(() => (query.data.value?.artifacts ?? []).filter((a) => !a.hidden));
const chosen = computed(() => {
  const data = query.data.value;
  if (!data) return undefined;
  const key = recommend(data.artifacts, environment.value, data.default_artifact);
  return visible.value.find((a) => a.key === key);
});
const label = (a: Artifact) =>
  [t(`releaseDownloads.os.${a.os}`), a.arch, a.format, a.scope, a.variant]
    .filter(Boolean)
    .join(" · ");
// The split button's two halves share one primary look.
const half =
  "inline-flex h-9 items-center gap-2 bg-primary text-sm font-medium text-primary-fg " +
  "transition-colors hover:bg-primary-hover focus-ring [&_svg]:size-4 [&_svg]:shrink-0";
</script>

<template>
  <Skeleton v-if="query.isPending.value" class="h-9 w-48 rounded-md" />
  <Button v-else-if="query.isError.value" @click="query.refetch()">
    <Download aria-hidden="true" />
    {{ t("releaseDownloads.retry") }}
  </Button>
  <Button v-else-if="!chosen" disabled>
    <Download aria-hidden="true" />
    {{ t("releaseDownloads.unavailable") }}
  </Button>
  <div v-else class="inline-flex shadow-sm" role="group" :aria-label="t('releaseDownloads.title')">
    <a :href="chosen.url" :class="[half, 'min-w-0 rounded-s-md px-4']">
      <Download aria-hidden="true" />
      <span class="whitespace-nowrap">{{ t("releaseDownloads.download") }}</span>
      <span class="truncate text-xs opacity-80">{{ label(chosen) }}</span>
    </a>
    <DropdownMenu align="end" content-class="max-h-96 overflow-y-auto">
      <template #trigger>
        <button
          type="button"
          :class="[half, 'rounded-e-md border-s border-primary-fg/25 px-2']"
          :aria-label="t('releaseDownloads.more')"
        >
          <ChevronDown aria-hidden="true" />
        </button>
      </template>
      <DropdownMenuLabel class="px-2 py-1.5 text-xs text-muted">
        {{ t("releaseDownloads.version", { version: query.data.value?.version }) }}
      </DropdownMenuLabel>
      <DropdownMenuSeparator class="-mx-1 my-1 h-px bg-border" />
      <DropdownMenuItem v-for="artifact in visible" :key="artifact.key" as-child>
        <a :href="artifact.url" class="justify-between gap-6">
          <span class="inline-flex items-center gap-2">
            <Check
              class="text-primary"
              :class="artifact.key === chosen.key ? 'visible' : 'invisible'"
              aria-hidden="true"
            />
            {{ label(artifact) }}
          </span>
          <span v-if="artifact.size !== undefined" class="text-xs text-muted">
            {{ format.bytes(artifact.size) }}
          </span>
        </a>
      </DropdownMenuItem>
    </DropdownMenu>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import type { Schema } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";
import { Badge, EntityIcon } from "@/shared/ui";

/**
 * An application tile. The name is the link (so its accessible name is just
 * the application name); a stretched overlay makes the whole card clickable.
 */
const props = withDefaults(defineProps<{ app: Schema<"PublicApp">; headingLevel?: 2 | 3 }>(), {
  headingLevel: 3,
});
const { t } = useI18n();
const localized = useLocalized();
const version = computed(() =>
  props.app.capabilities.versions ? props.app.latest_known_version?.version : undefined,
);
</script>

<template>
  <article
    class="relative flex h-full flex-col gap-3 rounded-xl border border-border bg-surface p-4 shadow-sm transition-colors hover:border-border-strong hover:bg-surface-hover has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-focus"
  >
    <div class="flex items-start gap-3">
      <EntityIcon :src="app.icon" size="lg" />
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <component :is="`h${headingLevel}`" class="truncate text-base font-semibold">
          <RouterLink
            :to="`/${app.key}`"
            class="outline-none after:absolute after:inset-0 after:rounded-xl hover:underline"
          >
            {{ localized(app.name) || app.id }}
          </RouterLink>
        </component>
        <p class="flex min-w-0 items-center gap-2 text-xs text-muted">
          <span class="truncate">{{ localized(app.vendor.name) || app.vendor.id }}</span>
          <Badge v-if="version" class="font-mono">
            <span class="sr-only">{{ t("catalog.app.latestVersion") }}</span>
            {{ version }}
          </Badge>
        </p>
      </div>
    </div>
    <p v-if="localized(app.description)" class="line-clamp-3 text-sm text-muted">
      {{ localized(app.description) }}
    </p>
  </article>
</template>

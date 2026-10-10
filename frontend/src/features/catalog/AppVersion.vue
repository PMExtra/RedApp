<script setup lang="ts">
import { I18nT, useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { Badge, RelativeTime } from "@/shared/ui";

/**
 * Newest locally known version and when RedApp first saw it. Only release
 * applications (`versions` capability) have versions.
 */
defineProps<{ app: Schema<"PublicApp"> }>();
const { t } = useI18n();
</script>

<template>
  <span v-if="app.capabilities.versions" class="inline-flex flex-wrap items-center gap-2">
    <template v-if="app.latest_known_version">
      <Badge tone="primary" class="font-mono">
        <span class="sr-only">{{ t("catalog.app.latestVersion") }}</span>
        {{ app.latest_known_version.version }}
      </Badge>
      <span v-if="app.latest_known_version.first_seen" class="text-xs text-muted">
        <I18nT keypath="catalog.app.firstSeen" scope="global">
          <template #time>
            <RelativeTime :value="app.latest_known_version.first_seen" />
          </template>
        </I18nT>
      </span>
    </template>
    <span v-else class="text-sm text-muted">{{ t("catalog.app.versionUnknown") }}</span>
  </span>
</template>

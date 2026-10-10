<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useBootstrap } from "@/features/bootstrap";
import { publicUrl } from "@/shared/lib";
import { Card, CodeBlock, Skeleton } from "@/shared/ui";

/**
 * Base address of a file-serving application without installers: clients
 * append a relative file path. Uses the effective public URL of the site.
 */
const props = defineProps<{ appKey: string }>();
const { t } = useI18n();
const bootstrap = useBootstrap();
const prefix = computed(() => {
  const base = bootstrap.data.value?.public_url;
  return base ? publicUrl(base, `/${props.appKey}/`) : "";
});
</script>

<template>
  <Card :title="t('catalog.prefix.title')" :description="t('catalog.prefix.description')">
    <CodeBlock v-if="prefix" :code="prefix" />
    <Skeleton v-else-if="bootstrap.isPending.value" class="h-11 w-full" />
    <p v-else class="text-sm text-muted">{{ t("catalog.prefix.unavailable") }}</p>
  </Card>
</template>

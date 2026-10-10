<script setup lang="ts">
import { computed } from "vue";
import { Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { confirm, toast } from "@/shared/lib";
import { Button, Card } from "@/shared/ui";
import { useDeleteApp, useDeleteVendor, type App, type Vendor } from "./queries";

/**
 * Permanent deletion of a custom vendor or application, after confirmation.
 * Built-in items cannot be deleted; the section explains that instead.
 */
const props = defineProps<{ entity: Vendor | App }>();
const { t } = useI18n();
const router = useRouter();
const deleteVendor = useDeleteVendor();
const deleteApp = useDeleteApp();
const isApp = computed(() => "provider" in props.entity);
const builtin = computed(() => {
  const entity = props.entity;
  return "provider" in entity ? entity.builtin_template : entity.has_template;
});
const key = computed(() => {
  const entity = props.entity;
  return "provider" in entity ? entity.key : entity.id;
});
const pending = computed(() => deleteVendor.isPending.value || deleteApp.isPending.value);

async function remove(): Promise<void> {
  const app = isApp.value;
  const confirmed = await confirm({
    title: t(app ? "directory.delete.appTitle" : "directory.delete.vendorTitle", {
      key: key.value,
    }),
    description: t(app ? "directory.delete.appWarning" : "directory.delete.vendorWarning"),
    confirmLabel: t("common.actions.delete"),
    tone: "danger",
  });
  if (!confirmed) return;
  const entity = props.entity;
  let cleanupPending = false;
  try {
    if ("provider" in entity) {
      cleanupPending = (await deleteApp.mutateAsync(entity)).cleanup_pending;
    } else {
      await deleteVendor.mutateAsync(entity);
    }
  } catch {
    return; // Reported by the global error handler or as a conflict notice.
  }
  toast({ tone: "success", title: t("directory.delete.done", { key: key.value }) });
  await router.push({
    name: "admin-vendors",
    query: cleanupPending ? { cleanup: "pending" } : {},
  });
}
</script>

<template>
  <Card :title="t('directory.delete.title')">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <p class="max-w-2xl text-sm text-muted">
        <template v-if="builtin">
          {{ t(isApp ? "directory.delete.builtinApp" : "directory.delete.builtinVendor") }}
        </template>
        <template v-else>
          {{ t(isApp ? "directory.delete.appWarning" : "directory.delete.vendorWarning") }}
        </template>
      </p>
      <Button v-if="!builtin" variant="danger" :loading="pending" @click="remove">
        <Trash2 aria-hidden="true" />
        {{ t(isApp ? "directory.delete.appAction" : "directory.delete.vendorAction") }}
      </Button>
    </div>
  </Card>
</template>

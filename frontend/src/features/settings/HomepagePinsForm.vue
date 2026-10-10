<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { X } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useDirtyGuard } from "@/shared/forms";
import { useLocalized } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import {
  AsyncState,
  Button,
  Card,
  IconButton,
  RevisionConflictAlert,
  SortableList,
} from "@/shared/ui";
import AppPicker from "./AppPicker.vue";
import { useHomepageSettings, useSaveHomepage, type AppListItem } from "./queries";

const MAX_PINS = 100;

const { t } = useI18n();
const localized = useLocalized();
const settings = useHomepageSettings();
const save = useSaveHomepage(settings.data);

// The saved order the draft was made from; `null` until loaded.
const base = ref<string[] | null>(null);
const draft = ref<string[]>([]);
// Names of applications chosen in this session; saved pins are shown by key.
const names = ref(new Map<string, string>());
const dirty = computed(
  () =>
    base.value !== null &&
    (draft.value.length !== base.value.length ||
      draft.value.some((key, index) => key !== base.value?.[index])),
);
useDirtyGuard(dirty);

function adopt(keys: readonly string[]) {
  base.value = [...keys];
  draft.value = [...keys];
}

// Adopt the server state unless the user has unsaved edits.
watch(
  () => settings.data.value,
  (state) => {
    if (state && !dirty.value) adopt(state.pinned_app_keys);
  },
  { immediate: true },
);

const full = computed(() => draft.value.length >= MAX_PINS);
const announcement = ref("");

function add(app: AppListItem) {
  if (draft.value.includes(app.key) || full.value) return;
  names.value.set(app.key, localized(app.name));
  draft.value = [...draft.value, app.key];
  announcement.value = t("settings.homepage.added", { name: itemLabel(app.key) });
}

function remove(key: string) {
  draft.value = draft.value.filter((item) => item !== key);
  announcement.value = t("settings.homepage.removed", { name: itemLabel(key) });
}

function itemLabel(key: string) {
  const name = names.value.get(key);
  return name ? `${name} (${key})` : key;
}

function submit() {
  save.mutate([...draft.value], {
    onSuccess: (state) => {
      adopt(state.pinned_app_keys);
      toast({ tone: "success", title: t("settings.homepage.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  if (settings.data.value) adopt(settings.data.value.pinned_app_keys);
}

function discard() {
  draft.value = [...(base.value ?? [])];
}
</script>

<template>
  <Card :title="t('settings.homepage.title')" :description="t('settings.homepage.description')">
    <AsyncState
      :loading="settings.isPending.value"
      :error="settings.data.value ? undefined : settings.error.value"
      @retry="settings.refetch()"
    >
      <!-- Not a <form>: Enter in the application search must not save. -->
      <div class="flex flex-col gap-4">
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <p
          v-if="draft.length === 0"
          class="rounded-lg border border-dashed border-border py-6 text-center text-sm text-muted"
        >
          {{ t("settings.homepage.empty") }}
        </p>
        <SortableList
          v-else
          v-model="draft"
          :item-key="(key) => key"
          :item-label="itemLabel"
          :disabled="save.isPending.value"
          role="group"
          :aria-label="t('settings.homepage.listLabel')"
        >
          <template #item="{ item, index }">
            <div class="flex items-center gap-3">
              <span class="w-6 text-end text-xs text-muted tabular-nums">{{ index + 1 }}</span>
              <span class="flex min-w-0 flex-1 flex-col">
                <span v-if="names.get(item)" class="truncate text-sm">{{ names.get(item) }}</span>
                <code class="truncate font-mono text-xs" :class="names.get(item) && 'text-muted'">
                  {{ item }}
                </code>
              </span>
              <IconButton
                size="sm"
                :label="t('settings.homepage.remove', { name: itemLabel(item) })"
                :disabled="save.isPending.value"
                @click="remove(item)"
              >
                <X aria-hidden="true" />
              </IconButton>
            </div>
          </template>
        </SortableList>
        <p class="sr-only" aria-live="polite">{{ announcement }}</p>
        <AppPicker
          :label="t('settings.homepage.add')"
          :description="
            full
              ? t('settings.homepage.full', { max: MAX_PINS })
              : t('settings.homepage.addHint', { count: draft.length, max: MAX_PINS })
          "
          :exclude="draft"
          :disabled="full || save.isPending.value"
          @select="add"
        />
        <div class="flex flex-wrap justify-end gap-2">
          <Button v-if="dirty" :disabled="save.isPending.value" @click="discard">
            {{ t("settings.discard") }}
          </Button>
          <Button
            variant="primary"
            :loading="save.isPending.value"
            :disabled="!dirty"
            @click="submit"
          >
            {{ t("settings.homepage.save") }}
          </Button>
        </div>
      </div>
    </AsyncState>
  </Card>
</template>

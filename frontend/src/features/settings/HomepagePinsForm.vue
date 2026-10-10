<script setup lang="ts">
import { computed, ref } from "vue";
import { X } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { same } from "@/features/configuration";
import { useLocalized } from "@/shared/i18n";
import { toast } from "@/shared/lib";
import {
  AsyncState,
  Badge,
  Button,
  Card,
  EntityIcon,
  IconButton,
  RevisionConflictAlert,
  SortableList,
} from "@/shared/ui";
import AppPicker from "./AppPicker.vue";
import {
  useHomepageSettings,
  useSaveHomepage,
  type AppListItem,
  type HomepagePinnedApp,
  type HomepageSettingsState,
} from "./queries";
import { useServerDraft } from "./serverDraft";

const MAX_PINS = 100;

const { t } = useI18n();
const localized = useLocalized();
const settings = useHomepageSettings();
const save = useSaveHomepage(settings.data);

// The saved order the draft was made from; `null` until loaded.
const base = ref<string[] | null>(null);
const draft = ref<string[]>([]);
// Display data by key: saved pins come from the server (`pinned_apps`),
// applications chosen in this session from the search result.
type PinState = HomepagePinnedApp["state"];
interface PinInfo {
  name: HomepagePinnedApp["name"];
  icon: string;
  state: PinState;
}
const info = ref(new Map<string, PinInfo>());
const dirty = computed(() => base.value !== null && !same(draft.value, base.value));

function remember(apps: readonly HomepagePinnedApp[]) {
  for (const app of apps) {
    info.value.set(app.key, {
      name: app.name,
      icon: app.icon ?? "",
      state: app.state,
    });
  }
}

// Display data is refreshed even while the order has unsaved edits.
const baseline = useServerDraft({
  state: () => settings.data.value,
  dirty: () => dirty.value,
  adopt: (state: HomepageSettingsState) => {
    remember(state.pinned_apps);
    base.value = [...state.pinned_app_keys];
    draft.value = [...state.pinned_app_keys];
  },
  keep: (state) => {
    remember(state.pinned_apps);
  },
});

const stateTones = { disabled: "warning", deleted: "danger", missing: "danger" } as const;

// Pins that are not on the public homepage right now.
function hidden(key: string) {
  const state = info.value.get(key)?.state;
  return state && state !== "published"
    ? { tone: stateTones[state], label: t(`settings.homepage.states.${state}`) }
    : null;
}

const full = computed(() => draft.value.length >= MAX_PINS);
const announcement = ref("");

function add(app: AppListItem) {
  if (draft.value.includes(app.key) || full.value) return;
  info.value.set(app.key, {
    name: app.name,
    icon: app.icon,
    state: app.deleted_at ? "deleted" : app.enabled ? "published" : "disabled",
  });
  draft.value = [...draft.value, app.key];
  announcement.value = t("settings.homepage.added", { name: itemLabel(app.key) });
}

function remove(key: string) {
  draft.value = draft.value.filter((item) => item !== key);
  announcement.value = t("settings.homepage.removed", { name: itemLabel(key) });
}

// Localized at render so a language switch renames the pins.
function pinName(key: string): string | null {
  const name = info.value.get(key)?.name;
  return name ? localized(name) : null;
}

function itemLabel(key: string) {
  const name = pinName(key);
  return name ? `${name} (${key})` : key;
}

function submit() {
  save.mutate([...draft.value], {
    onSuccess: (state) => {
      baseline.reset(state);
      toast({ tone: "success", title: t("settings.homepage.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  baseline.reset();
}

function discard() {
  baseline.reset();
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
              <EntityIcon :src="info.get(item)?.icon ?? ''" size="sm" />
              <span class="flex min-w-0 flex-1 flex-col">
                <span v-if="pinName(item)" class="truncate text-sm">
                  {{ pinName(item) }}
                </span>
                <code class="truncate font-mono text-xs" :class="pinName(item) && 'text-muted'">
                  {{ item }}
                </code>
              </span>
              <Badge v-if="hidden(item)" :tone="hidden(item)?.tone">
                {{ hidden(item)?.label }}
              </Badge>
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

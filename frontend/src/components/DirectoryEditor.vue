<script setup lang="ts">
import OverrideControl from "./OverrideControl.vue";
import ProxySection from "./ProxySection.vue";
import {
  getLeaf,
  setLeaf,
  type Configuration,
  type ProxyConfig,
} from "../configuration";
import DeleteConfirmation from "./DeleteConfirmation.vue";
import FilePicker from "./FilePicker.vue";
import EntityIcon from "./EntityIcon.vue";
import Icon from "./Icon.vue";
import IconButton from "./IconButton.vue";
import SortableList from "./SortableList.vue";
import TemplateReset from "./TemplateReset.vue";
import SwitchControl from "./SwitchControl.vue";
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import SelectMenu from "./SelectMenu.vue";
import { api, isCancellation } from "../api";
import { invalidateBootstrap, loadBootstrap } from "../bootstrap";
import {
  directoryIcon,
  patchEntityEnabled,
  refreshApplication,
  applicationPath,
  type ManagedApplication,
  type ProviderDefinition,
  type ProviderKey,
  type SourceStrategy,
  type Vendor,
} from "../directory";
import { useDirtyDraft } from "../composables/useDirtyDraft";
import { errorText, language, t } from "../i18n";
import type { LocalizedText } from "../site";
const props = defineProps<{ kind: "vendor" | "app" }>();
const emit = defineEmits<{ updated: [Vendor | ManagedApplication] }>();
const route = useRoute(),
  router = useRouter();
const creating = computed(() =>
  props.kind === "vendor" ? !route.params.vendor : !route.params.app,
);
const path = computed(() =>
  props.kind === "vendor"
    ? `vendors/${route.params.vendor || ""}`
    : `apps/${route.params.vendor}/${route.params.app || ""}`,
);
interface Draft {
  proxy: ProxyConfig;
  id: string;
  name: LocalizedText;
  description: LocalizedText;
  icon: string;
  localized_icons: LocalizedText;
  enabled: boolean;
  provider: ProviderKey;
  base_url: string;
  base_urls: string[];
  source_strategy: SourceStrategy;
  cache_ttl_seconds: number;
}
const empty = (): Draft => ({
  proxy: { mode: "inherit" },
  id: "",
  name: { en: "", "zh-CN": "" },
  description: { en: "", "zh-CN": "" },
  icon: "",
  localized_icons: { en: "", "zh-CN": "" },
  enabled: true,
  provider: "info",
  base_url: "",
  base_urls: [""],
  source_strategy: "",
  cache_ttl_seconds: 0,
});
const draft = ref<Draft>(),
  record = ref<Vendor | ManagedApplication>(),
  providers = ref<ProviderDefinition[]>([]);
const configuration = ref<Configuration>(),
  touched = ref(new Set<string>()),
  unsets = ref(new Set<string>());
const baseline = ref(""),
  loading = ref(false),
  saving = ref(false),
  toggling = ref(false),
  uploading = ref(false),
  saved = ref(false),
  error = ref<unknown>(),
  deleteReview = ref(false);
const dirty = computed(
  () =>
    !!draft.value &&
    (JSON.stringify(draft.value) !== baseline.value ||
      touched.value.size > 0 ||
      unsets.value.size > 0),
);
const confirmDiscard = useDirtyDraft(dirty);
const busy = computed(
  () => loading.value || saving.value || uploading.value || toggling.value,
);
const readOnly = computed(() => !!record.value?.deleted_at);
const languages = ["en", "zh-CN"] as const;
let ticket = 0,
  controller: AbortController | undefined;
function accept(value?: Vendor | ManagedApplication) {
  record.value = value;
  if (value) emit("updated", value);
  const fields = value
    ? {
        ...empty(),
        id: value.id,
        name: value.name,
        description: value.description,
        icon: value.icon,
        localized_icons:
          "provider" in value
            ? { en: "", "zh-CN": "" }
            : value.localized_icons || { en: "", "zh-CN": "" },
        enabled: value.enabled,
        ...("provider" in value
          ? {
              provider: value.provider,
              base_url: value.base_url,
              base_urls: value.base_urls?.length
                ? value.base_urls
                : [value.base_url],
              source_strategy: value.source_strategy || "ordered",
              cache_ttl_seconds: value.cache_ttl_seconds,
            }
          : {}),
      }
    : empty();
  if (configuration.value) Object.assign(fields, configuration.value.effective);
  draft.value = JSON.parse(JSON.stringify(fields)) as Draft;
  touched.value = new Set();
  unsets.value = new Set();
  baseline.value = JSON.stringify(fields);
}
async function load(confirm = true) {
  if (confirm && !confirmDiscard()) return;
  controller?.abort();
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  draft.value = undefined;
  configuration.value = undefined;
  touched.value = new Set();
  unsets.value = new Set();
  record.value = undefined;
  error.value = undefined;
  loading.value = true;
  saving.value = false;
  toggling.value = false;
  uploading.value = false;
  saved.value = false;
  deleteReview.value = false;
  try {
    const [definitions, response, config] = await Promise.all([
      props.kind === "app"
        ? api<{ providers: ProviderDefinition[] }>(
            "providers",
            undefined,
            request.signal,
          )
        : Promise.resolve(undefined),
      creating.value
        ? Promise.resolve(undefined)
        : api<{ vendor?: Vendor; app?: ManagedApplication }>(
            path.value,
            undefined,
            request.signal,
          ),
      creating.value
        ? Promise.resolve(undefined)
        : api<Configuration>(
            `${path.value}/configuration`,
            undefined,
            request.signal,
          ),
    ]);
    if (attempt !== ticket) return;
    configuration.value = config;
    providers.value =
      definitions?.providers.filter((provider) =>
        ["info", "hosted", "http-cache", "codex", "claude-code"].includes(
          provider.key,
        ),
      ) || [];
    if (props.kind === "app" && !providers.value.length)
      throw Error("Provider definitions unavailable");
    const value = response?.[props.kind];
    if (!creating.value && !value)
      throw Error("Application details unavailable");
    accept(value);
  } catch (reason) {
    if (attempt === ticket && !isCancellation(reason)) error.value = reason;
  } finally {
    if (attempt === ticket) {
      loading.value = false;
      controller = undefined;
    }
  }
}
function changeProvider(value: string) {
  if (!draft.value || !creating.value) return;
  draft.value.provider = value as ProviderKey;
  const definition = providers.value.find((provider) => provider.key === value);
  draft.value.base_url = definition?.default_base_url || "";
  draft.value.base_urls = [definition?.default_base_url || ""];
  draft.value.source_strategy = value === "http-cache" ? "ordered" : "";
  draft.value.cache_ttl_seconds =
    definition?.default_cache_ttl_seconds ??
    (value === "http-cache"
      ? 300
      : value === "info" || value === "hosted"
        ? 0
        : 60);
}
function mark(field: string) {
  touched.value.add(field);
  unsets.value.delete(field);
  saved.value = false;
}
function restore(field: string) {
  if (
    !draft.value ||
    !configuration.value?.defaults ||
    !configuration.value.template_ref
  )
    return;
  setLeaf(draft.value, field, getLeaf(configuration.value.defaults, field));
  touched.value.delete(field);
  unsets.value.add(field);
}
const editable = computed(() =>
  props.kind === "vendor"
    ? [
        "name.en",
        "name.zh-CN",
        "description.en",
        "description.zh-CN",
        "icon",
        "localized_icons.en",
        "localized_icons.zh-CN",
        "proxy",
      ]
    : [
        "name.en",
        "name.zh-CN",
        "description.en",
        "description.zh-CN",
        "icon",
        "proxy",
        ...(!draft.value || ["info", "hosted"].includes(draft.value.provider)
          ? []
          : draft.value.provider === "http-cache"
            ? ["base_urls", "source_strategy", "cache_ttl_seconds"]
            : ["base_url"]),
      ],
);
function fieldLabel(field: string) {
  if (field.startsWith("name."))
    return `${t("Name")} · ${field.endsWith("en") ? "English" : "简体中文"}`;
  if (field.startsWith("description."))
    return `${t("Description")} · ${field.endsWith("en") ? "English" : "简体中文"}`;
  return (
    (
      {
        icon: t("Icon"),
        "localized_icons.en": t("English logo"),
        "localized_icons.zh-CN": t("Chinese logo"),
        proxy: t("Upstream proxy"),
        base_url: t("Base URL"),
        base_urls: t("Upstream sources"),
        source_strategy: t("Source selection"),
        cache_ttl_seconds: t("Cache settings"),
      } as Record<string, string>
    )[field] || field
  );
}
function changedPatch() {
  const set: Record<string, unknown> = {};
  if (!draft.value) return { set, unset: [] as string[] };
  const original = JSON.parse(baseline.value);
  for (const field of editable.value) {
    if (unsets.value.has(field)) continue;
    if (
      touched.value.has(field) ||
      JSON.stringify(getLeaf(draft.value, field)) !==
        JSON.stringify(getLeaf(original, field))
    )
      set[field] = getLeaf(draft.value, field);
  }
  return { set, unset: [...unsets.value] };
}
async function synchronize() {
  await refreshApplication();
  invalidateBootstrap();
  void loadBootstrap();
}
async function changeEnabled(enabled: boolean) {
  if (!draft.value || busy.value || readOnly.value) return;
  if (creating.value) {
    draft.value.enabled = enabled;
    return;
  }
  if (!record.value || enabled === draft.value.enabled) return;
  const previous = draft.value.enabled,
    request = new AbortController(),
    attempt = ++ticket,
    kind = props.kind;
  controller = request;
  toggling.value = true;
  saved.value = false;
  error.value = undefined;
  draft.value.enabled = enabled;
  try {
    const response = await patchEntityEnabled(
      kind,
      kind === "vendor"
        ? record.value.id
        : (record.value as ManagedApplication).key,
      record.value.revision,
      enabled,
      request.signal,
    );
    if (attempt !== ticket) return;
    const value = response[kind];
    if (!value) throw Error("Saved record unavailable");
    record.value = value;
    if (configuration.value) configuration.value.revision = value.revision;
    emit("updated", value);
    draft.value.enabled = value.enabled;
    baseline.value = JSON.stringify({
      ...JSON.parse(baseline.value),
      enabled: value.enabled,
    });
    void synchronize();
  } catch (reason) {
    if (attempt === ticket) {
      draft.value!.enabled = previous;
      if (!isCancellation(reason)) error.value = reason;
    }
  } finally {
    if (attempt === ticket) {
      toggling.value = false;
      controller = undefined;
    }
  }
}
async function save(remove = false) {
  if (!draft.value || busy.value || (!remove && readOnly.value)) return;
  const patch = changedPatch();
  if (
    !remove &&
    !creating.value &&
    !Object.keys(patch.set).length &&
    !patch.unset.length
  )
    return;
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  saving.value = true;
  error.value = undefined;
  saved.value = false;
  const wasCreating = creating.value,
    kind = props.kind;
  const target = wasCreating
    ? kind === "vendor"
      ? "vendors"
      : `vendors/${route.params.vendor}/apps`
    : path.value;
  const {
    id,
    name,
    description,
    icon,
    enabled,
    provider,
    base_url,
    base_urls,
    source_strategy,
    cache_ttl_seconds,
  } = draft.value;
  const body = remove
    ? {
        revision: record.value?.revision,
        ...(kind === "app" ? { confirm_uid: record.value?.uid } : {}),
        confirm_key:
          kind === "vendor"
            ? record.value?.id
            : `${route.params.vendor}/${record.value?.id}`,
      }
    : {
        name,
        description,
        icon,
        ...(kind === "vendor"
          ? { localized_icons: draft.value.localized_icons }
          : {}),
        ...(wasCreating
          ? { id, enabled }
          : { revision: record.value?.revision }),
        ...(kind === "app"
          ? {
              ...(provider === "http-cache"
                ? {
                    base_urls: [...base_urls],
                    source_strategy,
                    cache_ttl_seconds,
                  }
                : provider === "info" || provider === "hosted"
                  ? {}
                  : { base_url }),
              ...(wasCreating ? { provider } : {}),
            }
          : {}),
      };
  try {
    let response: {
      vendor?: Vendor;
      app?: ManagedApplication;
      cleanup_pending?: boolean;
    };
    if (!wasCreating && !remove) {
      const config = await api<Configuration>(
        `${path.value}/configuration`,
        { revision: record.value?.revision, ...patch },
        request.signal,
        {},
        "PATCH",
      );
      if (attempt !== ticket) return;
      configuration.value = config;
      if (record.value) record.value.revision = config.revision;
      response = await api<{ vendor?: Vendor; app?: ManagedApplication }>(
        path.value,
        undefined,
        request.signal,
      );
    } else {
      response = await api(
        target,
        body,
        request.signal,
        {},
        remove ? "DELETE" : "POST",
      );
    }
    if (attempt !== ticket) return;
    if (remove) {
      baseline.value = JSON.stringify(draft.value);
      invalidateBootstrap();
      void loadBootstrap();
      await router.push({
        path: "/admin/vendors",
        query: response.cleanup_pending ? { cleanup: "pending" } : {},
      });
      return;
    }
    const value = response[kind];
    if (!value) throw Error("Saved record unavailable");
    accept(value);
    saved.value = true;
    deleteReview.value = false;
    await synchronize();
    if (attempt !== ticket) return;
    if (wasCreating)
      await router.replace(
        kind === "vendor"
          ? `/admin/vendors/${value.id}/settings`
          : applicationPath(value as ManagedApplication),
      );
  } catch (reason) {
    if (attempt === ticket && !isCancellation(reason)) error.value = reason;
  } finally {
    if (attempt === ticket) {
      saving.value = false;
      controller = undefined;
    }
  }
}
async function upload(event: Event, locale?: "en" | "zh-CN") {
  const input = event.target as HTMLInputElement,
    file = input.files?.[0];
  if (!file || !draft.value || busy.value || readOnly.value) return;
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  uploading.value = true;
  error.value = undefined;
  const body = new FormData();
  body.append("file", file);
  try {
    const response = await api<{ icon: string }>(
      "assets/icons",
      body,
      request.signal,
    );
    if (attempt === ticket && draft.value) {
      if (locale && props.kind === "vendor") {
        draft.value.localized_icons[locale] = response.icon;
        mark(`localized_icons.${locale}`);
      } else {
        draft.value.icon = response.icon;
        mark("icon");
      }
    }
  } catch (reason) {
    if (attempt === ticket && !isCancellation(reason)) error.value = reason;
  } finally {
    if (attempt === ticket) {
      uploading.value = false;
      controller = undefined;
      input.value = "";
    }
  }
}
watch([path, () => props.kind], () => void load(false), {
  immediate: true,
  flush: "sync",
});
onUnmounted(() => {
  ticket++;
  controller?.abort();
  draft.value = undefined;
});
</script>
<template>
  <section class="panel directory-editor" :data-kind="kind">
    <nav
      v-if="creating && kind === 'app'"
      class="breadcrumbs"
      :aria-label="t('Vendor sections')"
    >
      <RouterLink :to="`/admin/vendors/${route.params.vendor}/apps`"
        >{{ route.params.vendor }} / {{ t("Applications") }}</RouterLink
      >
    </nav>
    <div class="section-heading entity-editor-heading">
      <h2>
        {{
          creating
            ? kind === "vendor"
              ? t("Add vendor")
              : t("Add application")
            : kind === "vendor"
              ? t("Vendor details")
              : t("Application details")
        }}
      </h2>
      <div v-if="draft" class="entity-enabled-control">
        <SwitchControl
          :model-value="draft.enabled"
          @update:model-value="changeEnabled"
          name="enabled"
          :label="t('Enabled')"
          :disabled="busy || readOnly || loading"
          :title="
            kind === 'vendor'
              ? t(
                  'Disabling a vendor hides all its applications. Stored data is retained.',
                )
              : t(
                  'Disabled applications remain manageable here. Stored data is retained.',
                )
          "
          :aria-description="
            kind === 'vendor'
              ? t(
                  'Disabling a vendor hides all its applications. Stored data is retained.',
                )
              : t(
                  'Disabled applications remain manageable here. Stored data is retained.',
                )
          "
        />
        <small class="muted" role="status">{{
          toggling
            ? t("Saving…")
            : creating
              ? t("Saved on creation")
              : t("Saves immediately")
        }}</small>
      </div>
    </div>
    <p v-if="readOnly" class="notice" role="status">
      {{ t("Deleted. Stored data is retained; this record is read-only.") }}
    </p>
    <p v-if="saved && !readOnly" class="notice" role="status">
      {{ t("Changes saved.") }}
    </p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form @submit.prevent="save()">
      <fieldset v-if="draft" :disabled="busy || readOnly">
        <div class="entity-basics">
          <div class="entity-id">
            <label
              >{{ t("ID")
              }}<input
                v-model="draft.id"
                name="id"
                required
                pattern="[a-z0-9]+(-[a-z0-9]+)*"
                maxlength="63"
                :disabled="!creating"
                autocapitalize="none"
                spellcheck="false"
            /></label>
            <p v-if="creating" class="muted small-text">
              {{
                t(
                  "Use lowercase letters, numbers and single hyphens. This ID cannot be changed later.",
                )
              }}
            </p>
            <p v-if="!creating" class="muted small-text">
              {{
                kind === "vendor"
                  ? t("The vendor ID cannot be changed.")
                  : t("IDs, vendor and provider are fixed after creation.")
              }}
            </p>
          </div>
          <div v-if="kind === 'app'" class="field-label entity-provider">
            <span>{{ t("Provider") }}</span
            ><SelectMenu
              :model-value="draft.provider"
              :label="t('Provider')"
              :disabled="!creating"
              :options="
                providers.map((p) => ({
                  value: p.key,
                  label: p.name[language],
                  description: p.description?.[language],
                }))
              "
              @update:model-value="changeProvider"
            />
          </div>
          <p
            v-if="creating && kind === 'app'"
            class="muted small-text provider-description"
          >
            {{
              providers.find((p) => p.key === draft?.provider)?.description?.[
                language
              ]
            }}
          </p>
          <div class="icon-field" role="group" :aria-label="t('Icon')">
            <EntityIcon
              :src="directoryIcon(draft.icon)"
              size="detail"
              :vendor="kind === 'vendor'"
            />
            <FilePicker
              name="icon"
              :label="draft.icon ? t('Replace icon') : t('Choose icon')"
              accept="image/jpeg,image/png,image/svg+xml,.jpg,.jpeg,.png,.svg"
              :disabled="busy || readOnly"
              @change="upload"
            />
            <IconButton
              v-if="draft.icon"
              type="button"
              class="secondary"
              :disabled="busy || readOnly"
              @click="
                draft.icon = '';
                mark('icon');
              "
              icon="close"
              :label="t('Remove icon')"
            />
            <p v-if="uploading" role="status">{{ t("Uploading…") }}</p>
          </div>
          <details v-if="kind === 'vendor'" class="vendor-language-icons">
            <summary>{{ t("Language-specific logos (optional)") }}</summary>
            <p class="muted small-text">
              {{
                t(
                  "Use the current language logo when set; otherwise use the default logo.",
                )
              }}
            </p>
            <div
              v-for="locale in languages"
              :key="locale"
              class="icon-field"
              role="group"
              :aria-label="
                locale === 'en' ? t('English logo') : t('Chinese logo')
              "
            >
              <span>{{
                locale === "en" ? t("English logo") : t("Chinese logo")
              }}</span>
              <EntityIcon
                :src="directoryIcon(draft.localized_icons[locale])"
                vendor
              />
              <FilePicker
                :name="`icon-${locale}`"
                :label="
                  draft.localized_icons[locale]
                    ? t('Replace icon')
                    : t('Choose icon')
                "
                accept="image/jpeg,image/png,image/svg+xml,.jpg,.jpeg,.png,.svg"
                :disabled="busy || readOnly"
                @change="upload($event, locale)"
              />
              <IconButton
                v-if="draft.localized_icons[locale]"
                type="button"
                class="secondary"
                :disabled="busy || readOnly"
                @click="
                  draft.localized_icons[locale] = '';
                  mark(`localized_icons.${locale}`);
                "
                icon="close"
                :label="
                  locale === 'en'
                    ? t('Remove English logo')
                    : t('Remove Chinese logo')
                "
              />
            </div>
          </details>
        </div>
        <div class="two-columns">
          <fieldset v-for="lang in languages" :key="lang" class="site-locale">
            <legend>{{ lang === "en" ? "English" : "简体中文" }}</legend>
            <label
              >{{ t("Name")
              }}<input
                v-model="draft.name[lang]"
                @input="mark(`name.${lang}`)"
                :name="`name-${lang}`"
                :lang="lang"
                maxlength="64"
                required
            /></label>
            <label
              >{{ t("Description")
              }}<textarea
                v-model="draft.description[lang]"
                @input="mark(`description.${lang}`)"
                :name="`description-${lang}`"
                :lang="lang"
                maxlength="2000"
                rows="2"
              />
            </label>
          </fieldset>
        </div>
        <template v-if="kind === 'app'">
          <h3
            v-if="!['info', 'hosted'].includes(draft.provider)"
            class="settings-section-title"
          >
            {{ t("Sources and delivery") }}
          </h3>
          <template v-if="draft.provider === 'http-cache'">
            <div class="upstream-sources">
              <h3>{{ t("Upstream sources") }}</h3>
              <p class="muted small-text">
                {{
                  t(
                    "Add 1 to 16 HTTP or HTTPS directory URLs. Drag the left handle to reorder. Duplicate URLs are rejected.",
                  )
                }}
              </p>
              <SortableList
                v-model="draft.base_urls"
                @update:model-value="mark('base_urls')"
                class="source-url-list"
                :label="t('Upstream sources')"
                :item-label="
                  (_url, index) =>
                    t('Source URL {number}', { number: index + 1 })
                "
                :disabled="busy || readOnly"
              >
                <template #default="{ index }">
                  <div class="ordered-inline-row">
                    <label
                      >{{ t("Source URL {number}", { number: index + 1 })
                      }}<input
                        v-model="draft.base_urls[index]"
                        @input="mark('base_urls')"
                        :name="
                          index === 0 ? 'base_url' : `base_url_${index + 1}`
                        "
                        type="url"
                        required
                        maxlength="4096"
                        spellcheck="false"
                    /></label>
                    <IconButton
                      icon="close"
                      class="secondary"
                      :label="`${t('Remove source')}: ${index + 1}`"
                      :disabled="draft.base_urls.length === 1"
                      @click="
                        draft.base_urls.splice(index, 1);
                        mark('base_urls');
                      "
                    />
                  </div>
                </template>
              </SortableList>
              <IconButton
                type="button"
                class="secondary"
                :disabled="draft.base_urls.length >= 16"
                @click="
                  draft.base_urls.length < 16 && draft.base_urls.push('');
                  mark('base_urls');
                "
                icon="plus"
                :label="t('Add source')"
              />
            </div>
            <label
              >{{ t("Source selection")
              }}<SelectMenu
                v-model="draft.source_strategy"
                @update:model-value="mark('source_strategy')"
                name="source_strategy"
                :label="t('Source selection')"
                :options="[
                  { value: 'ordered', label: t('In order') },
                  { value: 'round_robin', label: t('Round robin') },
                  { value: 'random', label: t('Random') },
                ]"
            /></label>
            <label
              >{{ t("Default TTL without Cache-Control (seconds)")
              }}<input
                v-model.number="draft.cache_ttl_seconds"
                @input="mark('cache_ttl_seconds')"
                name="cache_ttl_seconds"
                type="number"
                min="0"
                max="86400"
                step="1"
                required
            /></label>
            <p class="muted small-text">
              {{
                t(
                  "Used only when no path rule matches and Cache-Control is absent. TTL 0 checks the origin every time and retains a complete copy; the stale fallback setting controls reuse on failure.",
                )
              }}
            </p>
          </template>
          <template v-else-if="!['info', 'hosted'].includes(draft.provider)"
            ><label
              >{{ t("Base URL")
              }}<input
                v-model="draft.base_url"
                @input="mark('base_url')"
                name="base_url"
                type="url"
                required
                spellcheck="false"
            /></label>
            <p class="muted small-text">
              {{
                t(
                  "The provider supplies a default upstream URL. You can replace it for this application.",
                )
              }}
            </p></template
          >
        </template>
        <template v-if="!creating">
          <ProxySection
            v-model="draft.proxy"
            :effective="configuration?.proxy_effective"
            :disabled="busy || readOnly"
            @update:model-value="mark('proxy')"
          />
          <div class="overlay-field-states">
            <label v-for="field in editable" :key="field"
              >{{ fieldLabel(field) }}
              <OverrideControl
                :configuration="configuration"
                :path="field"
                :custom="touched.has(field)"
                :restored="unsets.has(field)"
                :disabled="busy || readOnly"
                @restore="restore(field)"
                @customize="mark(field)"
            /></label>
          </div>
        </template>
      </fieldset>
      <div class="form-actions entity-save-actions">
        <button v-if="!readOnly" :disabled="busy || !draft">
          <Icon v-if="creating" name="plus" />
          {{ saving ? t("Saving…") : t("Save changes") }}
        </button>
        <IconButton
          type="button"
          class="secondary"
          :disabled="busy"
          @click="load()"
          icon="refresh"
          :label="t('Reload')"
        />
        <IconButton
          v-if="
            record && !('builtin_template' in record && record.builtin_template)
          "
          type="button"
          class="secondary delete-action"
          :disabled="busy"
          @click="deleteReview = !deleteReview"
          icon="trash"
          :label="t('Delete')"
        />
      </div>
    </form>
    <DeleteConfirmation
      v-if="deleteReview && record"
      :application="kind === 'app'"
      :record-key="
        kind === 'app' ? (record as ManagedApplication).key : record.id
      "
      :busy="busy"
      @confirm="save(true)"
      @cancel="deleteReview = false"
    />
    <TemplateReset
      v-if="kind === 'vendor' && record?.has_template && !readOnly"
      kind="vendor"
      :application="record.id"
      @saved="load(false)"
    />
  </section>
</template>

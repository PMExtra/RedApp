<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api";
import { invalidateBootstrap, loadBootstrap } from "../bootstrap";
import { loadDirectory, type ManagedApplication, type ProviderDefinition, type ProviderKey, type SourceStrategy, type Vendor } from "../directory";
import { useDirtyDraft } from "../composables/useDirtyDraft";
import { errorText, language, t } from "../i18n";
import type { LocalizedText } from "../site";
const props = defineProps<{ kind: "vendor" | "app" }>();
const route = useRoute(), router = useRouter();
const creating = computed(() => props.kind === "vendor" ? !route.params.vendor : !route.params.app);
const path = computed(() => props.kind === "vendor" ? `vendors/${route.params.vendor || ""}` : `apps/${route.params.vendor}/${route.params.app || ""}`);
interface Draft { id: string; name: LocalizedText; description: LocalizedText; icon: string; enabled: boolean; provider: ProviderKey; base_url: string; base_urls: string[]; source_strategy: SourceStrategy; cache_ttl_seconds: number }
const empty = (): Draft => ({ id: "", name: { en: "", "zh-CN": "" }, description: { en: "", "zh-CN": "" }, icon: "", enabled: true, provider: "general-http", base_url: "", base_urls: [""], source_strategy: "ordered", cache_ttl_seconds: 300 });
const draft = ref<Draft>(), record = ref<Vendor | ManagedApplication>(), providers = ref<ProviderDefinition[]>([]);
const baseline = ref(""), loading = ref(false), saving = ref(false), uploading = ref(false), saved = ref(false), error = ref<unknown>(), deleteReview = ref(false);
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== baseline.value);
const confirmDiscard = useDirtyDraft(dirty);
const busy = computed(() => loading.value || saving.value || uploading.value);
const readOnly = computed(() => !!record.value?.deleted_at);
const languages = ["en", "zh-CN"] as const;
const sourceList = ref<HTMLElement>();
let draggedSource: number | undefined;
let ticket = 0, controller: AbortController | undefined;
function accept(value?: Vendor | ManagedApplication) {
  record.value = value;
  const fields = value ? { ...empty(), id: value.id, name: value.name, description: value.description, icon: value.icon, enabled: value.enabled,
    ...("provider" in value ? { provider: value.provider, base_url: value.base_url, base_urls: value.base_urls?.length ? value.base_urls : [value.base_url], source_strategy: value.source_strategy || "ordered", cache_ttl_seconds: value.cache_ttl_seconds } : {}) } : empty();
  draft.value = JSON.parse(JSON.stringify(fields)) as Draft;
  baseline.value = JSON.stringify(fields);
}
async function load(confirm = true) {
  if (confirm && !confirmDiscard()) return;
  controller?.abort();
  const request = new AbortController(), attempt = ++ticket;
  controller = request;
  draft.value = undefined; record.value = undefined; error.value = undefined;
  loading.value = true; saving.value = false; uploading.value = false; saved.value = false; deleteReview.value = false; draggedSource = undefined;
  try {
    const [definitions, response] = await Promise.all([
      props.kind === "app" ? api<{ providers: ProviderDefinition[] }>("providers", undefined, request.signal) : Promise.resolve(undefined),
      creating.value ? Promise.resolve(undefined) : api<{ vendor?: Vendor; app?: ManagedApplication }>(path.value, undefined, request.signal),
    ]);
    if (attempt !== ticket) return;
    providers.value = definitions?.providers.filter((provider) => ["general-http", "codex", "claude-code"].includes(provider.key)) || [];
    if (props.kind === "app" && !providers.value.length) throw Error("Provider definitions unavailable");
    const value = response?.[props.kind];
    if (!creating.value && !value) throw Error("Application details unavailable");
    accept(value);
  } catch (reason) { if (attempt === ticket) error.value = reason; }
  finally { if (attempt === ticket) { loading.value = false; controller = undefined; } }
}
function changeProvider(event: Event) {
  if (!draft.value || !creating.value) return;
  const value = (event.target as HTMLSelectElement).value as ProviderKey;
  draft.value.provider = value;
  const definition = providers.value.find((provider) => provider.key === value);
  draft.value.base_url = definition?.default_base_url || "";
  draft.value.base_urls = [definition?.default_base_url || ""];
  draft.value.source_strategy = "ordered";
  draft.value.cache_ttl_seconds = definition?.default_cache_ttl_seconds ?? (value === "general-http" ? 300 : 60);
}
function moveSource(from: number, to: number, focus = true) {
  const urls = draft.value?.base_urls;
  if (!urls || busy.value || readOnly.value || from < 0 || to < 0 || from >= urls.length || to >= urls.length || from === to) return;
  const [url] = urls.splice(from, 1);
  urls.splice(to, 0, url!);
  if (focus) void nextTick(() => sourceList.value?.querySelectorAll<HTMLElement>('[data-source-row]')[to]?.querySelector<HTMLInputElement>('input')?.focus());
}
function beginDrag(index: number, event: DragEvent) {
  if (busy.value || readOnly.value) return;
  draggedSource = index;
  event.dataTransfer?.setData('text/plain', String(index));
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
}
function dropSource(index: number) {
  if (draggedSource !== undefined) moveSource(draggedSource, index);
  draggedSource = undefined;
}
async function synchronize() {
  await loadDirectory();
  invalidateBootstrap();
  void loadBootstrap();
}
async function save(remove = false) {
  if (!draft.value || busy.value || readOnly.value) return;
  const request = new AbortController(), attempt = ++ticket;
  controller = request; saving.value = true; error.value = undefined; saved.value = false;
  const wasCreating = creating.value, kind = props.kind;
  const target = wasCreating ? (kind === "vendor" ? "vendors" : `vendors/${route.params.vendor}/apps`) : path.value;
  const { id, name, description, icon, enabled, provider, base_url, base_urls, source_strategy, cache_ttl_seconds } = draft.value;
  const body = remove ? { revision: record.value?.revision } : {
    name, description, icon, enabled,
    ...(wasCreating ? { id } : { revision: record.value?.revision }),
    ...(kind === "app" ? { ...(provider === "general-http" ? { base_urls: [...base_urls], source_strategy, cache_ttl_seconds } : { base_url }), ...(wasCreating ? { provider } : {}) } : {}),
  };
  try {
    const response = await api<{ vendor?: Vendor; app?: ManagedApplication }>(target, body, request.signal, {}, remove ? "DELETE" : wasCreating ? "POST" : "PATCH");
    if (attempt !== ticket) return;
    const value = response[kind];
    if (!value) throw Error("Saved record unavailable");
    accept(value); saved.value = true; deleteReview.value = false;
    await synchronize();
    if (attempt !== ticket) return;
    if (wasCreating) await router.replace(kind === "vendor" ? `/admin/vendors/${value.id}/settings` : `/admin/apps/${(value as ManagedApplication).key}/settings`);
  } catch (reason) { if (attempt === ticket) error.value = reason; }
  finally { if (attempt === ticket) { saving.value = false; controller = undefined; } }
}
async function upload(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0];
  if (!file || !draft.value || busy.value || readOnly.value) return;
  const request = new AbortController(), attempt = ++ticket;
  controller = request; uploading.value = true; error.value = undefined;
  const body = new FormData(); body.append("file", file);
  try {
    const response = await api<{ icon: string }>("assets/icons", body, request.signal);
    if (attempt === ticket && draft.value) draft.value.icon = response.icon;
  } catch (reason) { if (attempt === ticket) error.value = reason; }
  finally { if (attempt === ticket) { uploading.value = false; controller = undefined; input.value = ""; } }
}
watch([path, () => props.kind], () => void load(false), { immediate: true, flush: "sync" });
onUnmounted(() => { ticket++; controller?.abort(); draft.value = undefined; });
</script>
<template>
  <section class="panel directory-editor" :data-kind="kind">
    <h2>{{ creating ? kind === 'vendor' ? t('Add vendor') : t('Add application') : kind === 'vendor' ? t('Vendor details') : t('Application details') }}</h2>
    <p v-if="!creating" class="muted">{{ t("IDs, vendor and provider are fixed after creation.") }}</p>
    <p v-if="readOnly" class="notice" role="status">{{ t("Deleted. Stored data is retained; this record is read-only.") }}</p>
    <p v-if="saved && !readOnly" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <form @submit.prevent="save()">
      <fieldset v-if="draft" :disabled="busy || readOnly">
        <label>{{ t("ID") }}<input v-model="draft.id" name="id" required pattern="[a-z0-9]+(-[a-z0-9]+)*" maxlength="63" :readonly="!creating" autocapitalize="none" spellcheck="false" /></label>
        <p v-if="creating" class="muted small-text">{{ t("Use lowercase letters, numbers and single hyphens. This ID cannot be changed later.") }}</p>
        <div class="two-columns"><fieldset v-for="lang in languages" :key="lang" class="site-locale"><legend>{{ lang === 'en' ? 'English' : '简体中文' }}</legend>
          <label>{{ t("Name") }}<input v-model="draft.name[lang]" :name="`name-${lang}`" :lang="lang" maxlength="64" required /></label>
          <label>{{ t("Description") }}<textarea v-model="draft.description[lang]" :name="`description-${lang}`" :lang="lang" maxlength="2000" rows="3" /></label>
        </fieldset></div>
        <div class="icon-field"><img v-if="draft.icon" :src="draft.icon" alt="" width="48" height="48" /><label>{{ t("Icon") }}<input type="file" name="icon" accept="image/jpeg,image/png,image/svg+xml,.jpg,.jpeg,.png,.svg" @change="upload" /></label><button v-if="draft.icon" type="button" class="secondary" @click="draft.icon = ''">{{ t("Remove icon") }}</button></div>
        <p v-if="uploading" role="status">{{ t("Uploading…") }}</p>
        <template v-if="kind === 'app'">
          <label>{{ t("Provider") }}<select :value="draft.provider" name="provider" :disabled="!creating" @change="changeProvider"><option v-for="provider in providers" :key="provider.key" :value="provider.key">{{ provider.name[language] }}</option></select></label>
          <template v-if="draft.provider === 'general-http'">
            <div class="upstream-sources"><h3>{{ t('Upstream sources') }}</h3><p class="muted small-text">{{ t('Add 1 to 16 HTTP or HTTPS directory URLs. Drag to reorder, or use Move up and Move down. Duplicate URLs are rejected.') }}</p>
              <ol ref="sourceList" class="source-url-list"><li v-for="(_url, index) in draft.base_urls" :key="index" data-source-row @dragover.prevent @drop.prevent="dropSource(index)">
                <label>{{ t('Source URL {number}', { number: index + 1 }) }}<input v-model="draft.base_urls[index]" :name="index === 0 ? 'base_url' : `base_url_${index + 1}`" type="url" required maxlength="4096" spellcheck="false" /></label>
                <div class="form-actions"><button type="button" class="secondary source-drag" :draggable="!busy && !readOnly" @dragstart="beginDrag(index, $event)" @dragend="draggedSource = undefined">{{ t('Drag to reorder') }}</button><button type="button" class="secondary" :disabled="index === 0" @click="moveSource(index, index - 1)">{{ t('Move up') }}</button><button type="button" class="secondary" :disabled="index === draft.base_urls.length - 1" @click="moveSource(index, index + 1)">{{ t('Move down') }}</button><button type="button" class="secondary" :disabled="draft.base_urls.length === 1" @click="draft.base_urls.splice(index, 1)">{{ t('Remove source') }}</button></div>
              </li></ol>
              <button type="button" class="secondary" :disabled="draft.base_urls.length >= 16" @click="draft.base_urls.length < 16 && draft.base_urls.push('')">{{ t('Add source') }}</button>
            </div>
            <label>{{ t('Source selection') }}<select v-model="draft.source_strategy" name="source_strategy"><option value="ordered">{{ t('In order') }}</option><option value="round_robin">{{ t('Round robin') }}</option><option value="random">{{ t('Random') }}</option></select></label>
            <label>{{ t('Default TTL without Cache-Control (seconds)') }}<input v-model.number="draft.cache_ttl_seconds" name="cache_ttl_seconds" type="number" min="0" max="86400" step="1" required /></label>
            <p class="muted small-text">{{ t('Used only when no path rule matches and Cache-Control is absent. TTL 0 checks the origin every time and retains a complete copy; the stale fallback setting controls reuse on failure.') }}</p>
          </template>
          <template v-else><label>{{ t("Base URL") }}<input v-model="draft.base_url" name="base_url" type="url" required spellcheck="false" /></label><p class="muted small-text">{{ t("The provider supplies a default upstream URL. You can replace it for this application.") }}</p></template>
        </template>
        <label class="checkbox-field"><input v-model="draft.enabled" name="enabled" type="checkbox" />{{ t("Enabled") }}</label>
        <p class="muted small-text">{{ kind === 'vendor' ? t("Disabling a vendor hides all its applications. Stored data is retained.") : t("Disabled applications remain manageable here. Stored data is retained.") }}</p>
      </fieldset>
      <div class="form-actions">
        <button v-if="!readOnly" :disabled="busy || !draft">{{ saving ? t("Saving…") : t("Save changes") }}</button>
        <button type="button" class="secondary" :disabled="busy" @click="load()">{{ t("Reload") }}</button>
        <button v-if="record && !readOnly" type="button" class="secondary" :disabled="busy" @click="deleteReview = !deleteReview">{{ t("Delete") }}</button>
      </div>
    </form>
    <div v-if="deleteReview" class="delete-review" role="group" :aria-label="t('Confirm deletion')">
      <p>{{ t("Deletion disables access and keeps stored data. The ID remains reserved.") }}</p>
      <p v-if="kind === 'vendor'" class="muted">{{ t("Delete applications under this vendor first.") }}</p>
      <button class="danger" :disabled="busy" @click="save(true)">{{ t("Confirm deletion") }}</button>
      <button class="secondary" :disabled="busy" @click="deleteReview = false">{{ t("Cancel") }}</button>
    </div>
  </section>
</template>

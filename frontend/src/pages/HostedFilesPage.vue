<script setup lang="ts">
import IconButton from "../components/IconButton.vue";
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { applicationRecord } from "../directory";
import { api, bytes } from "../api";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import { useDirtyDraft } from "../composables/useDirtyDraft";
import { downloadPath, type HostedFile } from "../hosted";
import { errorText, localDate, t } from "../i18n";
import PageNavigation from "../components/PageNavigation.vue";
import SelectMenu from "../components/SelectMenu.vue";
const application = computed(() => applicationRecord.value?.key || "");
const endpoint = computed(() => `apps/${application.value}/files`);
const list = reactive(useNumberedCollection<HostedFile>(endpoint));
const mode = ref("upload"),
  relative = ref(""),
  source = ref(""),
  selected = ref<File>(),
  expected = ref(""),
  busy = ref(false),
  error = ref<unknown>(),
  message = ref(""),
  deleteID = ref(""),
  deleting = ref(false);
const progress = ref<{ bytes: number; total: number; state: string }>();
let controller: AbortController | undefined,
  progressController: AbortController | undefined,
  timer: ReturnType<typeof setTimeout> | undefined,
  transferID = "",
  ticket = 0;
useDirtyDraft(
  computed(
    () => busy.value || !!relative.value || !!source.value || !!selected.value,
  ),
);
function chooseFile(event: Event) {
  selected.value = (event.target as HTMLInputElement).files?.[0];
  if (selected.value && !relative.value) relative.value = selected.value.name;
}
function clear() {
  relative.value = "";
  source.value = "";
  selected.value = undefined;
  expected.value = "";
}
function replace(row: HostedFile) {
  clear();
  relative.value = row.path;
  expected.value = row.id;
  message.value = "";
}
function stop() {
  ticket++;
  clearTimeout(timer);
  controller?.abort();
  progressController?.abort();
  controller = undefined;
  progressController = undefined;
  busy.value = false;
}
async function poll(attempt: number) {
  if (attempt !== ticket || !busy.value) return;
  const request = new AbortController();
  progressController = request;
  try {
    const next = await api<{ bytes: number; total: number; state: string }>(
      `${endpoint.value}/transfers/${transferID}`,
      undefined,
      request.signal,
    );
    if (attempt === ticket) progress.value = next;
  } catch {
    /* The upload may still be accepting its headers, or have just completed. */
  } finally {
    if (attempt === ticket && busy.value)
      timer = setTimeout(() => void poll(attempt), 750);
  }
}
async function save() {
  if (
    busy.value ||
    !relative.value ||
    (mode.value === "upload" ? !selected.value : !source.value)
  )
    return;
  stop();
  const attempt = ++ticket,
    request = new AbortController();
  controller = request;
  busy.value = true;
  error.value = undefined;
  message.value = "";
  progress.value = {
    bytes: 0,
    total: selected.value?.size || -1,
    state: "receiving",
  };
  transferID = crypto.randomUUID().replaceAll("-", "");
  timer = setTimeout(() => void poll(attempt), 250);
  try {
    let body: unknown,
      url = `${endpoint.value}?transfer_id=${transferID}`;
    if (mode.value === "upload") {
      const form = new FormData();
      form.append("path", relative.value);
      form.append("expected_id", expected.value);
      form.append("file", selected.value!);
      body = form;
    } else {
      url = `${endpoint.value}/import?transfer_id=${transferID}`;
      body = {
        path: relative.value,
        url: source.value,
        expected_id: expected.value,
      };
    }
    await api<HostedFile>(url, body, request.signal);
    if (attempt !== ticket) return;
    clear();
    message.value = t("File saved.");
    void list.refresh();
  } catch (reason) {
    if (attempt === ticket) error.value = reason;
  } finally {
    if (attempt === ticket) {
      clearTimeout(timer);
      progressController?.abort();
      busy.value = false;
      controller = undefined;
    }
  }
}
async function cancel() {
  const target = `${endpoint.value}/transfers/${transferID}`;
  stop();
  message.value = t(
    "Transfer cancellation requested. Refreshing saved files.",
  );
  try {
    await api(target, {}, undefined, {}, "DELETE");
  } catch {
    /* The disconnected request also cancels the server operation. */
  } finally {
    void list.refresh();
  }
}
async function remove() {
  if (!deleteID.value || deleting.value) return;
  deleting.value = true;
  error.value = undefined;
  const attempt = ticket;
  try {
    await api(
      `${endpoint.value}/${deleteID.value}`,
      {},
      undefined,
      {},
      "DELETE",
    );
    if (attempt === ticket) {
      deleteID.value = "";
      void list.refresh();
    }
  } catch (reason) {
    if (attempt === ticket) error.value = reason;
  } finally {
    if (attempt === ticket) deleting.value = false;
  }
}
watch(
  application,
  () => {
    stop();
    clear();
    deleteID.value = "";
    error.value = undefined;
    message.value = "";
  },
  { flush: "sync" },
);
onUnmounted(stop);
</script>
<template>
  <div class="page-stack hosted-workspace">
    <section v-if="!applicationRecord?.deleted_at" class="panel">
      <h2>{{ t("Add a file") }}</h2>
      <p class="muted">
        {{
          t(
            "Files stay available until you delete or explicitly replace them. Import URLs are used only once.",
          )
        }}
      </p>
      <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
      <p v-if="message" role="status" class="notice">{{ message }}</p>
      <form @submit.prevent="save">
        <fieldset :disabled="busy">
          <div class="field-label">
            <span>{{ t("Source") }}</span
            ><SelectMenu
              v-model="mode"
              :label="t('Source')"
              :options="[
                { value: 'upload', label: t('Upload file') },
                { value: 'url', label: t('Import from URL') },
              ]"
            />
          </div>
          <label
            >{{ t("Resource path")
            }}<input
              v-model="relative"
              name="resource_path"
              required
              maxlength="4096"
              :readonly="!!expected"
              placeholder="releases/tool.zip"
          /></label>
          <p v-if="expected" class="notice">
            {{
              t(
                "This explicitly replaces the selected file. A newer change will be rejected.",
              )
            }}
            <button type="button" class="secondary" @click="clear">
              {{ t("Cancel replacement") }}
            </button>
          </p>
          <label v-if="mode === 'upload'"
            >{{ t("Upload file")
            }}<input
              type="file"
              name="hosted_file"
              :key="selected?.name || 'empty'"
              @change="chooseFile" /></label
          ><label v-else
            >{{ t("HTTP(S) URL")
            }}<input
              v-model="source"
              name="import_url"
              type="url"
              required
              maxlength="8192"
              autocomplete="off"
              spellcheck="false"
          /></label>
          <div class="form-actions">
            <button :disabled="mode === 'upload' ? !selected : !source">
              {{ expected ? t("Replace file") : t("Save file") }}
            </button>
          </div>
        </fieldset>
      </form>
      <div v-if="busy" class="transfer-progress" role="status">
        <progress
          :value="
            progress?.total && progress.total > 0 ? progress.bytes : undefined
          "
          :max="
            progress?.total && progress.total > 0 ? progress.total : undefined
          "
        /><span
          >{{ t("Saving file") }} · {{ bytes(progress?.bytes || 0)
          }}<template v-if="progress && progress.total > 0">
            / {{ bytes(progress.total) }}</template
          ></span
        ><button class="secondary" @click="cancel">{{ t("Cancel") }}</button>
      </div>
    </section>
    <section class="panel">
      <h2>{{ t("Hosted files") }}</h2>
      <p v-if="list.error" class="error" role="alert">
        {{ errorText(list.error) }}
      </p>
      <p v-if="list.loaded && !list.items.length">
        {{ t("No files saved yet.") }}
      </p>
      <div
        v-if="list.items.length"
        class="table-wrap"
        tabindex="0"
        :aria-label="t('Hosted files')"
      >
        <table>
          <thead>
            <tr>
              <th>{{ t("Resource") }}</th>
              <th>{{ t("Size") }}</th>
              <th>{{ t("Saved at") }}</th>
              <th>{{ t("Actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="file in list.items" :key="file.id">
              <td>
                <a :href="downloadPath(application, file.path)" download>{{
                  file.path
                }}</a>
              </td>
              <td>{{ bytes(file.size_bytes) }}</td>
              <td>{{ localDate(file.created_at) }}</td>
              <td>
                <div class="form-actions">
                  <button
                    class="secondary"
                    :disabled="
                      busy || deleting || !!applicationRecord?.deleted_at
                    "
                    @click="replace(file)"
                  >
                    {{ t("Replace file") }}</button
                  ><IconButton
                    class="secondary"
                    :disabled="busy || deleting"
                    @click="deleteID = file.id"
                    icon="trash"
                    :label="t('Delete')"
                  />
                </div>
                <div v-if="deleteID === file.id" class="delete-review">
                  <p>
                    {{
                      t(
                        "Delete this saved file? It cannot be downloaded again unless you add it.",
                      )
                    }}
                  </p>
                  <button class="danger" :disabled="deleting" @click="remove">
                    {{ t("Confirm deletion") }}</button
                  ><button class="secondary" @click="deleteID = ''">
                    {{ t("Cancel") }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <PageNavigation
        :label="t('File pages')"
        :page="list.page"
        :total="list.total"
        :total-pages="list.totalPages"
        :previous="list.previousAvailable"
        :next="list.nextAvailable"
        :loading="list.loading"
        @previous="list.previous"
        @next="list.next"
        @go="list.go"
        @refresh="list.refresh"
      />
    </section>
  </div>
</template>

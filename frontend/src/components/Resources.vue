<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { bytes, type Resource, type VersionSummary } from "../api";
import { appAPI } from "../bootstrap";
import { usePagedCollection } from "../composables/usePagedCollection";
import { errorText, localDate, stateLabel, t } from "../i18n";
import SelectMenu from "./SelectMenu.vue";
import PageNavigation from "./PageNavigation.vue";
const props = withDefaults(
  defineProps<{ application: string; automatic?: boolean }>(),
  { automatic: true },
);
const version = ref("");
const automatic = computed(() => props.automatic);
watch(
  () => props.application,
  () => {
    version.value = "";
  },
  { flush: "sync" },
);
const versions = reactive(
  usePagedCollection<VersionSummary>(
    computed(() => `${appAPI(props.application)}/versions`),
    automatic,
  ),
);
const resources = reactive(
  usePagedCollection<Resource>(
    computed(
      () =>
        `${appAPI(props.application)}/resources${version.value ? `?version=${encodeURIComponent(version.value)}` : ""}`,
    ),
    automatic,
  ),
);
const versionOptions = computed(() => [
  { value: "", label: t("All versions") },
  ...(version.value &&
  !versions.items.some((item) => item.version === version.value)
    ? [{ value: version.value, label: version.value }]
    : []),
  ...versions.items.map((item) => ({
    value: item.version,
    label: item.version,
  })),
]);
</script>
<template>
  <section class="panel">
    <div class="section-heading">
      <h2>{{ t("Versions and resources") }}</h2>
      <div class="inline-label">
        <span>{{ t("Version") }}</span
        ><SelectMenu
          v-model="version"
          :label="t('Version')"
          :options="versionOptions"
        />
      </div>
    </div>
    <div v-if="versions.error" class="error" role="alert">
      {{ errorText(versions.error)
      }}<small v-if="versions.loaded">{{
        t("Showing the last successful snapshot.")
      }}</small
      ><button
        class="secondary"
        :disabled="versions.loading"
        @click="versions.refresh"
      >
        {{ t("Retry") }}
      </button>
    </div>
    <p v-if="!versions.loaded && versions.loading" role="status">
      {{ t("Loading…") }}
    </p>
    <div class="version-list">
      <article v-for="item in versions.items" :key="item.version">
        <button
          class="version-button secondary"
          :aria-pressed="version === item.version"
          @click="version = version === item.version ? '' : item.version"
        >
          {{ item.version }}
        </button>
        <div>
          <span>{{ t("First seen") }} {{ localDate(item.first_seen) }}</span
          ><small class="muted">{{
            t("{count} artifact requests", {
              count: item.requests,
            })
          }}</small>
        </div>
      </article>
      <p v-if="versions.loaded && !versions.items.length" class="empty">
        {{ t("No versions discovered yet. Downloads are fetched on demand.") }}
      </p>
    </div>
    <PageNavigation
      :label="t('Version pages')"
      :page="versions.page"
      :previous="versions.previousAvailable"
      :next="versions.nextAvailable"
      :loading="versions.loading"
      @previous="versions.previous"
      @next="versions.next"
      @refresh="versions.refresh"
    />
    <h3>{{ t("Resources") }}</h3>
    <div v-if="resources.error" class="error" role="alert">
      {{ errorText(resources.error)
      }}<small v-if="resources.loaded">{{
        t("Showing the last successful snapshot.")
      }}</small
      ><button
        class="secondary"
        :disabled="resources.loading"
        @click="resources.refresh"
      >
        {{ t("Retry") }}
      </button>
    </div>
    <p v-if="!resources.loaded && resources.loading" role="status">
      {{ t("Loading…") }}
    </p>
    <p class="muted small-text">
      {{
        t(
          "Average effective speed includes retries and excludes verification; recent speed is a five-second snapshot.",
        )
      }}
    </p>
    <div
      class="table-wrap"
      v-if="resources.items.length"
      tabindex="0"
      :aria-label="t('Versions and resources')"
    >
      <table>
        <thead>
          <tr>
            <th>{{ t("Version / file") }}</th>
            <th>{{ t("Generation / state") }}</th>
            <th>{{ t("On disk / readers") }}</th>
            <th>{{ t("Average / recent") }}</th>
            <th>{{ t("Resumes / verification") }}</th>
            <th>{{ t("Started / finished") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in resources.items" :key="item.ID">
            <td>
              <strong>{{ item.Resource.Version }}</strong
              ><code class="file-name">{{ item.Resource.Key }}</code>
            </td>
            <td>
              <code>{{ item.ID.slice(0, 8) }}</code
              ><span
                :class="[
                  'state-badge',
                  {
                    'state-success': item.State === 'complete',
                    'state-error': ['failed', 'invalid'].includes(item.State),
                  },
                ]"
                >{{ stateLabel(item.State) }}</span
              ><small v-if="item.Retired">{{ t("Pending deletion") }}</small>
            </td>
            <td>
              {{ bytes(item.Bytes)
              }}<small>{{ t("Active readers") }}: {{ item.Readers }}</small>
            </td>
            <td>
              {{ bytes(item.AverageBPS) }}/s<small
                >{{ bytes(item.RecentBPS) }}/s</small
              >
            </td>
            <td>
              {{ item.Resumes }} /
              {{ (item.VerificationNS / 1e6).toFixed(1) }} ms
            </td>
            <td>
              <time>{{ localDate(item.Started) }}</time
              ><small>{{ localDate(item.Finished) }}</small>
              <details v-if="item.Error">
                <summary>{{ t("Last error") }}</summary>
                <p class="diagnostic">{{ item.Error }}</p>
              </details>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="resources.loaded" class="empty">
      {{ t("No cached resources for this selection.") }}
    </p>
    <PageNavigation
      :label="t('Resource pages')"
      :page="resources.page"
      :previous="resources.previousAvailable"
      :next="resources.nextAvailable"
      :loading="resources.loading"
      @previous="resources.previous"
      @next="resources.next"
      @refresh="resources.refresh"
    />
  </section>
</template>

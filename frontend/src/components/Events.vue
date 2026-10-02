<script setup lang="ts">
import type { DistributionEvent } from "../api";
import { localDate, t } from "../i18n";
import Icon from "./Icon.vue";
defineProps<{ events: DistributionEvent[] }>();
</script>
<template>
  <section class="panel">
    <div class="section-heading">
      <h2>{{ t("Recent failures") }}</h2>
      <span class="count-badge">{{ events.length }}</span>
    </div>
    <div v-if="!events.length" class="empty-state">
      <Icon name="check" :size="30" />
      <p>{{ t("No recent failures.") }}</p>
    </div>
    <template v-else
      ><p class="muted small-text">
        {{
          t("Original diagnostic messages are shown as reported by the server.")
        }}
      </p>
      <div class="table-wrap" tabindex="0" :aria-label="t('Recent failures')">
        <table>
          <thead>
            <tr>
              <th>{{ t("Time") }}</th>
              <th>{{ t("Category / status") }}</th>
              <th>{{ t("Application") }} / {{ t("Version") }}</th>
              <th>{{ t("Resource") }}</th>
              <th>{{ t("Message") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(event, index) in events" :key="index">
              <td>{{ localDate(event.time) }}</td>
              <td>
                <code>{{ event.category }}</code
                ><small v-if="event.code">{{ event.code }}</small
                ><span v-if="event.status_code" class="state-badge state-error"
                  >HTTP {{ event.status_code }}</span
                >
              </td>
              <td>
                <code>{{ event.app_id || "—" }}</code
                ><small>{{ event.version }}</small>
              </td>
              <td>
                <code class="file-name">{{
                  event.resource_key || event.resource
                }}</code
                ><small v-if="event.generation_id">{{
                  event.generation_id
                }}</small>
              </td>
              <td class="diagnostic">{{ event.message }}</td>
            </tr>
          </tbody>
        </table>
      </div></template
    >
  </section>
</template>

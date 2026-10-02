<script setup lang="ts">
import type { Status } from "../api";
import { localDate, t } from "../i18n";
import Icon from "./Icon.vue";
defineProps<{ events: Status["events"] }>();
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
              <th>{{ t("Resource") }}</th>
              <th>{{ t("Message") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(event, index) in events" :key="index">
              <td>{{ localDate(event.time) }}</td>
              <td>
                <code>{{ event.category }}</code
                ><span v-if="event.status_code" class="state-badge state-error"
                  >HTTP {{ event.status_code }}</span
                >
              </td>
              <td>
                <code class="file-name">{{ event.resource }}</code>
              </td>
              <td class="diagnostic">{{ event.message }}</td>
            </tr>
          </tbody>
        </table>
      </div></template
    >
  </section>
</template>

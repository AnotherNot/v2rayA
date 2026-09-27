<script setup lang="ts">
// The rule-source panel, shared by the routing and the DNS dialogs: a URL, the
// set it last served, and the import. Both sources have the same shape, so one
// component covers them rather than one panel each.
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { mdiDeleteOutline, mdiOpenInNew } from "@mdi/js";
import type { RoutingSource, RoutingSourceItem } from "@/api/types";

const props = defineProps<{
  /** what the panel is a source of, for the heading and the messages */
  subject: "routing" | "dns";
  source: RoutingSource | null;
  /** whether the source's index offers a choice of sets; the DNS one does not */
  picksSet: boolean;
  importSource: (url: string, remarks?: string) => Promise<void>;
  clearSource: () => Promise<void>;
}>();

const { t } = useI18n();
const link = ref("");
const remarks = ref("");
const chosen = ref("");
const touched = ref(false);
const busy = ref(false);

const source = computed(() => props.source);
const configured = computed(() => !!source.value?.url);
// A source that is not set up yet has nothing to collapse, so its form is open
// on arrival; a configured one stays closed until it is asked for.
const expanded = ref(!configured.value);
const version = computed(() => source.value?.version ?? "");
const items = computed<RoutingSourceItem[]>(
  () => (source.value?.items ?? []) as RoutingSourceItem[],
);
const invalid = computed(
  () => link.value.trim() !== "" && !link.value.trim().startsWith("http"),
);

// Opening a dialog must not overwrite a configured URL with an empty field, and
// a reload must not discard an edit in progress.
watch(
  () => [source.value?.url, source.value?.remarks] as const,
  ([url, value]) => {
    if (touched.value) return;
    link.value = url ?? "";
    remarks.value = value ?? "";
    // The set in use is the default choice, so importing again without
    // touching the picker re-imports what is already in force.
    if (value) chosen.value = value;
    else if (items.value.length) chosen.value = items.value[0].remarks;
  },
  { immediate: true },
);
watch(
  () => items.value,
  (list) => {
    if (list.length && !chosen.value) chosen.value = list[0].remarks;
  },
  { immediate: true },
);

async function run() {
  if (busy.value || invalid.value || !link.value.trim()) return;
  busy.value = true;
  try {
    await props.importSource(link.value.trim(), chosen.value || undefined);
    touched.value = false;
  } finally {
    busy.value = false;
  }
}

async function clear() {
  if (busy.value) return;
  busy.value = true;
  try {
    await props.clearSource();
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <v-sheet rounded="lg" bg-color="surface-container-low" class="mb-4">
    <v-expansion-panels
      variant="accordion"
      flat
      :model-value="expanded ? 0 : undefined"
      @update:model-value="expanded = $event === 0"
    >
      <v-expansion-panel :elevation="0" :value="0">
        <v-expansion-panel-title>
          <div class="d-flex align-center text-body-medium">
            <v-icon :icon="mdiOpenInNew" class="me-2" size="small" />
            {{ t("ruleSource.title") }}
            <span
              v-if="configured"
              class="text-on-surface-variant ms-2 text-caption"
            >
              {{ source?.remarks || t("ruleSource.firstSet") }}
            </span>
            <span v-else class="text-on-surface-variant ms-2 text-caption">
              {{ t("ruleSource.configure") }}
            </span>
          </div>
        </v-expansion-panel-title>
        <v-expansion-panel-text>
          <p class="md3-body-medium mb-3 text-on-surface-variant">
            {{ t(`ruleSource.${subject}.hint`) }}
          </p>
          <v-text-field
            v-model="link"
            :label="t('ruleSource.url')"
            placeholder="https://example.com/rules.json"
            dir="ltr"
            :error-messages="invalid ? t('ruleSource.wrongUrl') : undefined"
            :disabled="busy"
            @update:model-value="touched = true"
          />
          <v-select
            v-if="picksSet && items.length"
            v-model="chosen"
            :items="items.map((item) => item.remarks)"
            :label="t('ruleSource.set')"
            :disabled="busy"
          />
          <div class="text-caption text-on-surface-variant mb-2">
            <template v-if="version">
              {{ t("ruleSource.version", { version }) }}
            </template>
            <template v-if="subject === 'routing' && source?.domainStrategy">
              {{
                t("ruleSource.strategy", { strategy: source.domainStrategy })
              }}
            </template>
          </div>
          <div class="d-flex align-center ga-2">
            <v-btn
              variant="flat"
              color="primary"
              :loading="busy"
              :disabled="invalid || !link"
              @click="run"
            >
              {{ t("ruleSource.import") }}
            </v-btn>
            <v-spacer />
            <v-btn
              variant="text"
              color="error"
              :icon="mdiDeleteOutline"
              :disabled="busy"
              :title="t('ruleSource.forget')"
              @click="clear"
            />
          </div>
          <div class="text-caption text-on-surface-variant mt-2">
            {{ t(`ruleSource.${subject}.note`) }}
          </div>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
  </v-sheet>
</template>

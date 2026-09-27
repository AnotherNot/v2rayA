<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { mdiDeleteOutline, mdiPlus } from "@mdi/js";
import { getDnsRules, getOutbounds, putDnsRules } from "@/api";
import { deleteDnsSource, getDnsSource, putDnsSource } from "@/api";
import type { DnsRule, RuleSourceResult, RoutingSource } from "@/api/types";
import { errorText } from "@/api/errors";
import DocsLink from "@/components/DocsLink.vue";
import { useNotify } from "@/composables";
import RuleSource from "./RuleSource.vue";

defineOptions({ name: "DnsDialog" });
const emit = defineEmits<{ close: [] }>();
const { t } = useI18n();
const notify = useNotify();
const defaults: DnsRule[] = [
  { server: "localhost", domains: "geosite:private", outbound: "direct" },
  { server: "223.5.5.5", domains: "geosite:cn", outbound: "direct" },
  { server: "8.8.8.8", domains: "", outbound: "proxy" },
];
const rules = ref<DnsRule[]>(defaults.map((rule) => ({ ...rule })));
const outbounds = ref(["proxy"]);
const choices = computed(() => [...new Set(["direct", ...outbounds.value])]);
const loading = ref(true);
const loaded = ref(false);
const saving = ref(false);
const loadError = ref("");
const dnsSource = ref<RoutingSource | null>(null);

// Fields set outside the dialog (the DNS module's matchers) survive a save;
// `upstream` and `domain` are the migrated aliases of the two edited fields
// and the generator prefers them, so they fold into the form and are not sent
// back.
function foldRules(incoming: Partial<DnsRule>[] | null | undefined): DnsRule[] {
  return (incoming ?? []).map(({ upstream, domain, ...rule }) => ({
    ...rule,
    server: (upstream as string) || rule.server || "",
    domains: (domain as string) || rule.domains || "",
    outbound: rule.outbound || "direct",
  }));
}

onMounted(async () => {
  try {
    const [dns, groups] = await Promise.all([getDnsRules(), getOutbounds()]);
    if (dns.rules?.length) rules.value = foldRules(dns.rules);
    outbounds.value = groups.outbounds;
    loaded.value = true;
    // A source that cannot be read must not keep the rules from being edited.
    try {
      dnsSource.value = await getDnsSource();
    } catch {
      dnsSource.value = null;
    }
  } catch (err) {
    loadError.value = errorText(err);
    notify.warning(loadError.value);
  } finally {
    loading.value = false;
  }
});

function resetDefault() {
  rules.value = defaults.map((rule) => ({ ...rule }));
}

async function importFromSource(url: string) {
  try {
    const res: RuleSourceResult = await putDnsSource({ url });
    const dns = await getDnsRules();
    rules.value = foldRules(dns.rules);
    dnsSource.value = await getDnsSource();
    notify.success(
      res.alreadyUpToDate
        ? t("ruleSource.alreadyUpToDate")
        : t("ruleSource.imported"),
    );
  } catch (err) {
    notify.warning(errorText(err));
    throw err;
  }
}

async function clearSource() {
  try {
    await deleteDnsSource();
    dnsSource.value = { url: "", version: "", remarks: "", domainStrategy: "" };
    notify.success(t("ruleSource.forgotten"));
  } catch (err) {
    notify.warning(errorText(err));
    throw err;
  }
}

async function save() {
  if (!loaded.value || saving.value) return;
  const validRules = rules.value.filter((rule) => rule.server.trim() !== "");
  if (!validRules.length) {
    notify.warning(t("dns.errNoRules"));
    return;
  }
  saving.value = true;
  try {
    await putDnsRules(validRules);
    emit("close");
  } catch (err) {
    notify.warning(t("dns.saveFailed", { message: errorText(err) }));
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <v-card tag="form" rounded="xl" @submit.prevent="save">
    <v-card-item class="px-6 pt-6 pb-2">
      <v-card-title class="md3-headline-small pa-0 d-flex align-center ga-1">
        {{ t("dns.title") }}
        <DocsLink section="transparent-proxy" new-tab />
      </v-card-title>
    </v-card-item>
    <v-card-text class="px-6">
      <v-skeleton-loader v-if="loading" type="article" />
      <v-alert v-else-if="loadError" type="warning" variant="tonal">
        {{ loadError }}
      </v-alert>
      <template v-else>
        <RuleSource
          subject="dns"
          :source="dnsSource"
          :picks-set="false"
          :import-source="importFromSource"
          :clear-source="clearSource"
        />
        <v-sheet
          v-for="(rule, index) in rules"
          :key="index"
          color="surface-container-low"
          rounded="lg"
          class="pa-4 mb-3"
        >
          <div class="d-flex align-center mb-3">
            <span class="md3-title-small">
              {{ t("dns.rule", { n: index + 1 }) }}
            </span>
            <v-spacer />
            <v-tooltip :text="t('operations.delete')">
              <template #activator="{ props }">
                <v-btn
                  v-bind="props"
                  icon
                  variant="text"
                  type="button"
                  size="40"
                  :aria-label="t('operations.delete')"
                  :disabled="saving"
                  @click="rules.splice(index, 1)"
                >
                  <v-icon :icon="mdiDeleteOutline" size="20" />
                </v-btn>
              </template>
            </v-tooltip>
          </div>
          <v-row dense>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="rule.server"
                :label="t('dns.colServer')"
                :placeholder="t('dns.serverPlaceholder')"
                :disabled="saving"
                hide-details="auto"
                dir="ltr"
              />
            </v-col>
            <v-col cols="12" sm="6">
              <v-select
                v-model="rule.outbound"
                :label="t('dns.colOutbound')"
                :items="choices"
                :disabled="saving"
                hide-details="auto"
                dir="ltr"
              />
            </v-col>
            <v-col cols="12">
              <v-textarea
                v-model="rule.domains"
                :label="t('dns.colDomains')"
                :placeholder="t('dns.domainsPlaceholder')"
                :disabled="saving"
                rows="2"
                auto-grow
                hide-details="auto"
                dir="ltr"
              />
            </v-col>
          </v-row>
        </v-sheet>
        <div class="d-flex flex-wrap ga-2">
          <v-btn
            variant="tonal"
            type="button"
            :prepend-icon="mdiPlus"
            :disabled="saving"
            @click="rules.push({ server: '', domains: '', outbound: 'direct' })"
          >
            {{ t("dns.addRule") }}
          </v-btn>
          <v-btn
            variant="text"
            type="button"
            :disabled="saving"
            @click="resetDefault"
          >
            {{ t("dns.resetDefault") }}
          </v-btn>
        </div>
      </template>
    </v-card-text>
    <v-card-actions class="px-6 pb-4">
      <v-spacer />
      <v-btn variant="text" type="button" @click="emit('close')">
        {{ t("operations.cancel") }}
      </v-btn>
      <v-btn
        variant="flat"
        color="primary"
        type="submit"
        :loading="saving"
        :disabled="!loaded"
      >
        {{ t("operations.save") }}
      </v-btn>
    </v-card-actions>
  </v-card>
</template>

// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { DOMWrapper, flushPromises, type VueWrapper } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { VApp, VMain } from "vuetify/components";
import {
  getAppRouter,
  getDnsRules,
  getOutbounds,
  getRemoteGFWListVersion,
  getSetting,
  putSetting,
} from "@/api";
import type { AppRouterCounters, Setting, VersionResponse } from "@/api/types";
import DialogHost from "@/components/hosts/DialogHost.vue";
import { closeAllDialogs } from "@/composables/useDialog";
import { useAppStore } from "@/stores/app";
import { mountWithApp } from "@/test/mount";
import SettingsView from "../SettingsView.vue";

vi.mock("@/api", () => ({
  getSetting: vi.fn(),
  getAppRouter: vi.fn(),
  getRemoteGFWListVersion: vi.fn(),
  putSetting: vi.fn(),
  getDnsRules: vi.fn(),
  getOutbounds: vi.fn(),
  putDnsRules: vi.fn(),
}));

const loaded: Setting = {
  transparent: "proxy",
  transparentType: "tproxy",
  ipforward: true,
  portSharing: false,
  portSharingUsername: "",
  portSharingPassword: "",
  tproxyExcludedInterfaces: "docker*,veth*",
  tunAutoRoute: true,
  tunRouteShellType: "bash",
  tunRouteShellPath: "/bin/bash",
  tunSetupScript: "ip route add default dev tun0",
  tunTeardownScript: "ip route del default dev tun0",
  tunScriptAcknowledged: false,
  tunExcludeProcesses: "firefox,chrome",
  appRouterUnknownOwner: "direct",
  pacMode: "blocklist",
  pacAutoUpdateMode: "auto_update_at_intervals",
  pacAutoUpdateIntervalHour: 24,
  subscriptionAutoUpdateMode: "auto_update_at_intervals",
  subscriptionAutoUpdateIntervalHour: 12,
  proxyModeWhenSubscribe: "direct",
  tcpFastOpen: "default",
  logLevel: "info",
  inboundSniffing: "http,tls",
  routeOnly: true,
  muxOn: "yes",
  mux: 8,
};

let wrapper: VueWrapper;
beforeEach(() => {
  vi.mocked(getSetting).mockResolvedValue({
    // the DNS mode is the service's, and the settings page never sends it back
    setting: { ...loaded, dnsMode: "service" },
    localBlocklistVersion: "2026-09-15",
    localGeositeVersion: "",
  });
  vi.mocked(getRemoteGFWListVersion).mockResolvedValue({
    remoteBlocklistVersion: "2026-09-15",
  });
  vi.mocked(putSetting).mockResolvedValue(undefined);
  vi.mocked(getDnsRules).mockResolvedValue({ rules: [] });
  vi.mocked(getOutbounds).mockResolvedValue({ outbounds: ["proxy"] });
  vi.mocked(getAppRouter).mockResolvedValue({
    mode: false,
    attached: false,
    rules: 0,
    classes: 0,
  });
});
afterEach(() => {
  closeAllDialogs();
  wrapper?.unmount();
  vi.clearAllMocks();
});

async function mountPage() {
  wrapper = mountWithApp(
    defineComponent({
      setup: () => () =>
        h(VApp, null, () => [
          h(VMain, null, () => h(SettingsView)),
          h(DialogHost),
        ]),
    }),
  );
  useAppStore().version = {
    os: "linux",
    isRoot: true,
    tunSupported: true,
  } as VersionResponse;
  await flushPromises();
}

async function choose(label: string, option: string) {
  await wrapper.get(`button[aria-label^="${label}:"]`).trigger("click");
  await flushPromises();
  const item = [...document.querySelectorAll('[role="menuitemradio"]')].find(
    (entry) => entry.textContent?.trim() === option,
  );
  expect(item, `${label}: ${option}`).toBeDefined();
  await new DOMWrapper(item!).trigger("click");
  await flushPromises();
}

async function toggle(label: string, value: boolean) {
  await wrapper.get(`input[aria-label="${label}"]`).setValue(value);
  await flushPromises();
}

describe("settings list", () => {
  test("edits settings through the list and preserves the save request contract", async () => {
    await mountPage();
    await choose("Log Level", "Debug");
    await wrapper
      .get('input[aria-label="Update Blocklist Data Regularly (Unit: hour)"]')
      .setValue("48");
    await wrapper.get('input[aria-label="Concurrency"]').setValue("16");
    // the dashboard's quick controls are here too, in the same form
    await toggle("Port Sharing", true);
    await choose("Transparent Proxy/System Proxy", "Off");
    expect(
      wrapper.find('input[aria-label="Excluded Interface Prefixes"]').exists(),
    ).toBe(false);
    await wrapper.get('button[type="submit"]').trigger("click");
    await flushPromises();
    expect(putSetting).toHaveBeenCalledExactlyOnceWith(
      {
        ...loaded,
        transparent: "close",
        portSharing: true,
        logLevel: "debug",
        pacAutoUpdateIntervalHour: 48,
        mux: 16,
      },
      { signal: expect.any(AbortSignal) },
    );
  });

  test("shows dependent rows and blocks saving invalid intervals", async () => {
    await mountPage();
    await choose("Sniffing", "Off");
    expect(wrapper.find('input[aria-label="RouteOnly"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Domains Excluded");
    await toggle("Multiplex", false);
    expect(wrapper.find('input[aria-label="Concurrency"]').exists()).toBe(
      false,
    );
    await wrapper
      .get('input[aria-label="Update Blocklist Data Regularly (Unit: hour)"]')
      .setValue("0");
    await wrapper.get('button[type="submit"]').trigger("click");
    await flushPromises();
    expect(putSetting).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("Required");
    await wrapper
      .get('input[aria-label="Update Blocklist Data Regularly (Unit: hour)"]')
      .setValue("12");
    await wrapper.get('button[type="submit"]').trigger("click");
    await flushPromises();
    expect(putSetting).toHaveBeenCalledOnce();
  });

  test("leaves the DNS mode to the DNS settings dialog", async () => {
    await mountPage();
    // the proxy section carries no DNS decision of its own; only the row that
    // opens the dialog mentions DNS
    expect(wrapper.findAll('button[aria-label*="DNS"]')).toHaveLength(0);
    const row = wrapper
      .findAll(".v-list-item")
      .find((item) => item.text().startsWith("DNS Settings"));
    expect(row).toBeDefined();
    await row!.trigger("click");
    await flushPromises();
    const dialog = new DOMWrapper(document.querySelector('[role="dialog"]')!);
    expect(
      dialog.findAllComponents({ name: "VSelect" })[0].props("modelValue"),
    ).toBe("service");
    closeAllDialogs();
    await toggle("Port Sharing", true);
    await wrapper.get('button[type="submit"]').trigger("click");
    await flushPromises();
    // a stale mode here would put back the decision the dialog has moved
    expect(putSetting).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ portSharing: true }),
      { signal: expect.any(AbortSignal) },
    );
    const body = vi.mocked(putSetting).mock.calls[0][0];
    expect(body).not.toHaveProperty("dnsMode");
    expect(body).not.toHaveProperty("dnsHijack");
  });

  test("opens the existing About content as a dismissible dialog", async () => {
    await mountPage();
    const about = wrapper
      .findAll(".v-list-item")
      .find((row) => row.find(".v-list-item-title").text() === "About");
    await about!.trigger("click");
    await flushPromises();
    const dialog = new DOMWrapper(document.querySelector('[role="dialog"]')!);
    expect(dialog.text()).toContain("Founded by @mzz2017");
    expect(dialog.find('a[href$="/discussions"]').exists()).toBe(true);
    const close = dialog
      .findAll("button")
      .find((button) => button.text() === "Close");
    await close!.trigger("click");
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  test("shows the standard GeoSite date without treating it as a GFWList version", async () => {
    vi.mocked(getSetting).mockResolvedValue({
      setting: { ...loaded },
      localBlocklistVersion: "",
      localGeositeVersion: "2026-09-16",
    });
    vi.mocked(getRemoteGFWListVersion).mockResolvedValue({
      remoteBlocklistVersion: "2026-09-15",
    });
    await mountPage();
    expect(wrapper.text()).toContain("Local: 2026-09-16 (geosite)");
    expect(wrapper.text()).not.toContain(
      "Based on modified time of file which sometimes is after latest version online.",
    );
  });

  test("a load that failed offers a retry instead of a form of defaults", async () => {
    vi.mocked(getSetting).mockRejectedValue(new Error("backend is down"));
    await mountPage();
    // the form would otherwise sit on its defaults looking like stored values
    expect(wrapper.text()).toContain("backend is down");
    expect(wrapper.text()).not.toContain("Transparent Proxy");

    vi.mocked(getSetting).mockResolvedValue({
      setting: { ...loaded },
      localBlocklistVersion: "2026-09-15",
      localGeositeVersion: "",
    });
    const retry = wrapper
      .findAll("button")
      .find((b) => b.text().trim() === "Update");
    expect(retry).toBeDefined();
    await retry!.trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain("backend is down");
    expect(wrapper.text()).toContain("Transparent Proxy");
  });
});

const noCounters: AppRouterCounters = {
  connects: 0,
  implicitDirect: 0,
  self: 0,
  foreignNetns: 0,
  decide: 0,
  noRule: 0,
  ruleDirect: 0,
  ruleProxy: 0,
  ruleBlock: 0,
  redirected: 0,
  portWritten: 0,
  getpeername: 0,
  exempt: 0,
  rulesFull: 0,
  connects6: 0,
  udp6Refused: 0,
  udpDatagrams: 0,
  udpRedirected: 0,
  udpNoFlow: 0,
  udpRewritten: 0,
  udpAnswers: 0,
  relayed: 0,
  relayDirect: 0,
  refused: 0,
  udpRelayed: 0,
  udpDirect: 0,
  udpRefused: 0,
};

// The counters are the only way to tell an attached engine that routes
// nothing from an engine that is not there, so the page shows both the state
// and what the engine counted.
test("shows the running app router's counters", async () => {
  vi.mocked(getSetting).mockResolvedValue({
    setting: { ...loaded, transparentType: "app_router" },
    localBlocklistVersion: "2026-09-15",
    localGeositeVersion: "",
  });
  vi.mocked(getAppRouter).mockResolvedValue({
    mode: true,
    attached: true,
    rules: 3,
    classes: 1,
    counters: {
      ...noCounters,
      connects: 120,
      ruleProxy: 96,
      ruleDirect: 20,
      ruleBlock: 4,
      decide: 12,
    },
  });

  await mountPage();

  expect(wrapper.text()).toContain("attached");
  expect(wrapper.text()).toContain("rules: 3");
  expect(wrapper.text()).toContain("classes: 1");
  expect(wrapper.text()).toContain("connects: 120");
  expect(wrapper.text()).toContain("proxied: 96");
  expect(wrapper.text()).toContain("decided late: 12");
});

// A mode that is selected but not applied says so instead of showing zeros,
// which would look like an engine that saw no traffic.
test("says the app router engine is not attached", async () => {
  vi.mocked(getSetting).mockResolvedValue({
    setting: { ...loaded, transparentType: "app_router" },
    localBlocklistVersion: "2026-09-15",
    localGeositeVersion: "",
  });

  await mountPage();

  expect(wrapper.text()).toContain("not attached");
  expect(wrapper.text()).toContain("press Start");
  expect(wrapper.text()).not.toContain("0 connects");
});

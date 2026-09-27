// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { flushPromises, type VueWrapper } from "@vue/test-utils";
import type * as Api from "@/api";
import { getOutbound, putOutbound } from "@/api";
import { mountWithApp } from "@/test/mount";
import OutboundGroup from "./OutboundGroup.vue";

vi.mock("@/api", async (original) => ({
  ...(await original<typeof Api>()),
  getOutbound: vi.fn(),
  putOutbound: vi.fn(),
}));

let wrapper: VueWrapper;
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(putOutbound).mockResolvedValue(undefined);
});
afterEach(() => wrapper?.unmount());

test("a group that fails to load closes instead of offering the defaults for saving", async () => {
  vi.mocked(getOutbound).mockRejectedValue(new Error("backend is down"));

  wrapper = mountWithApp(OutboundGroup, { props: { outbound: "proxy" } });
  await flushPromises();

  // Saving the defaults over a group whose real settings never loaded would
  // replace a working probe URL and interval with empty strings.
  expect(vi.mocked(putOutbound)).not.toHaveBeenCalled();
  expect(wrapper.emitted("close")).toEqual([[]]);
});

test("a loaded group can be saved", async () => {
  vi.mocked(getOutbound).mockResolvedValue({
    setting: {
      probeURL: "https://probe.test",
      probeInterval: "10s",
      type: "leastping",
    },
  });

  wrapper = mountWithApp(OutboundGroup, { props: { outbound: "proxy" } });
  await flushPromises();

  expect(wrapper.emitted("close")).toBeUndefined();
  const save = wrapper
    .findAll("button")
    .find((b) => b.text().trim() === "Save");
  expect(save).toBeDefined();
  await save!.trigger("click");
  await flushPromises();

  expect(vi.mocked(putOutbound)).toHaveBeenCalledWith({
    outbound: "proxy",
    setting: {
      probeURL: "https://probe.test",
      probeInterval: "10s",
      type: "leastping",
    },
  });
  expect(wrapper.emitted("close")).toEqual([[true]]);
});

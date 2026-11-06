// @vitest-environment happy-dom
import { describe, expect, test } from "vitest";
import {
  generateShareLink,
  parseShareLink,
  standardXhttp,
} from "./serverCodec";
import { links } from "./__fixtures__/links";
import expected from "./__fixtures__/serverCodec.json";

// The expected values are what the editor produced before the codec
// moved out of modalServer.vue: this suite pins the move, not a spec of
// the share-link formats (the http branch, for one, generates
// http-proxy:// and parses http://, and that stays as it is).
describe("serverCodec keeps the editor's behaviour", () => {
  for (const [name, link] of Object.entries(links)) {
    test(`${name}: link → form`, () => {
      expect(parseShareLink(link)).toEqual(
        expected[name as keyof typeof expected].form,
      );
    });
    test(`${name}: form → link`, () => {
      const form = parseShareLink(link);
      expect(form && generateShareLink(form)).toEqual(
        expected[name as keyof typeof expected].back,
      );
    });
  }

  test("vless encryption and reality pqv survive a round trip", () => {
    const link =
      "vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?encryption=mlkem768x25519plus.native.0rtt.AAAA&security=reality&sni=example.com&fp=chrome&pbk=K&sid=ab&spx=%2F&pqv=VERIFY&type=tcp#n";
    const form = parseShareLink(link)!;
    expect(form.encryption).toBe("mlkem768x25519plus.native.0rtt.AAAA");
    expect(form.pqv).toBe("VERIFY");
    const back = generateShareLink(form)!;
    expect(back).toContain("encryption=mlkem768x25519plus.native.0rtt.AAAA");
    expect(back).toContain("pqv=VERIFY");
  });

  test("an unknown scheme is null both ways", () => {
    expect(parseShareLink("gopher://x")).toBeNull();
    expect(generateShareLink({ protocol: "gopher" })).toBeNull();
  });
});

test("a vless xhttp link keeps the core's padding fields through parse and generate", () => {
  const link =
    "vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?encryption=none&security=tls&sni=example.com&type=xhttp&path=%2Fx&xhttpMode=packet-up&xPaddingObfsMode=true&xPaddingKey=pk&xPaddingHeader=Referer&xPaddingPlacement=cookie&xPaddingMethod=tokenish";
  const fields = [
    "xPaddingObfsMode",
    "xPaddingKey",
    "xPaddingHeader",
    "xPaddingPlacement",
    "xPaddingMethod",
  ] as const;
  const model = parseShareLink(link) as Record<string, unknown>;
  expect(fields.map((f) => model[f])).toEqual([
    true,
    "pk",
    "Referer",
    "cookie",
    "tokenish",
  ]);
  const out = generateShareLink(model);
  for (const f of fields) expect(out).toContain(`${f}=${model[f]}`);
  const again = parseShareLink(out!) as Record<string, unknown>;
  expect(fields.map((f) => again[f])).toEqual([
    true,
    "pk",
    "Referer",
    "cookie",
    "tokenish",
  ]);
});

test("a wireguard link keeps workers, reserved and kernelMode through parse and generate", () => {
  const link =
    "wireguard://cHJpdmF0ZQ@1.2.3.4:51820?publicKey=cHVi&address=10.0.0.2%2F32&reserved=1%2C2%2C3&workers=4&kernelMode=true#wg";
  const model = parseShareLink(link) as Record<string, unknown>;
  expect(model.reserved).toBe("1,2,3");
  expect(model.workers).toBe("4");
  expect(model.kernelMode).toBe(true);
  const out = generateShareLink(model);
  expect(out).toContain("reserved=1%2C2%2C3");
  expect(out).toContain("workers=4");
  expect(out).toContain("kernelMode=true");
});

describe("standard XHTTP parameters", () => {
  test("mode, x_padding_bytes and extra fill the GUI fields", () => {
    expect(
      standardXhttp({ mode: "packet-up", x_padding_bytes: "100-1000" }),
    ).toEqual({ mode: "packet-up", paddingFrom: "100", paddingTo: "1000" });
    expect(
      standardXhttp({
        extra: JSON.stringify({ mode: "stream-up", xPaddingBytes: "10-20" }),
      }),
    ).toEqual({ mode: "stream-up", paddingFrom: "10", paddingTo: "20" });
    expect(standardXhttp({ x_padding_bytes: "500" })).toEqual({
      mode: "",
      paddingFrom: "0",
      paddingTo: "500",
    });
    expect(standardXhttp({})).toEqual({
      mode: "",
      paddingFrom: "",
      paddingTo: "",
    });
    expect(standardXhttp({ x_padding_bytes: "nope" })).toEqual({
      mode: "",
      paddingFrom: "",
      paddingTo: "",
    });
    expect(standardXhttp({ extra: "{broken" })).toEqual({
      mode: "",
      paddingFrom: "",
      paddingTo: "",
    });
  });

  test("a link with an extra object keeps its mode and padding", () => {
    const extra = encodeURIComponent(
      JSON.stringify({ mode: "stream-up", xPaddingBytes: "10-20" }),
    );
    const form = parseShareLink(
      `vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?security=tls&type=xhttp&path=%2Fx&extra=${extra}#extra`,
    ) as Record<string, unknown>;
    expect(form.xhttpMode).toBe("stream-up");
    expect(form.xPaddingBytesFrom).toBe("10");
    expect(form.xPaddingBytesTo).toBe("20");
  });

  test("an encryption value survives parse and generate", () => {
    const form = parseShareLink(
      "vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?encryption=mlkem.testonly&security=tls&type=tcp#enc",
    ) as Record<string, unknown>;
    expect(form.encryption).toBe("mlkem.testonly");
    expect(generateShareLink(form)).toContain("encryption=mlkem.testonly");
  });
});

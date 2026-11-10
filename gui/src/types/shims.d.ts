declare module "js-base64" {
  export const Base64: {
    encode(s: string, urlsafe?: boolean): string;
    encodeURI(s: string): string;
    decode(s: string): string;
  };
}

declare module "urijs" {
  interface URI {
    protocol(value: string): URI;
    username(value: string): URI;
    password(value: string): URI;
    host(value: string): URI;
    port(value: string | number): URI;
    path(value: string): URI;
    query(value: Record<string, unknown>): URI;
    hash(value: string): URI;
    toString(): string;
  }
  export default function URI(): URI;
}

// qrcode ships no types; the calls the sharing dialog and the e2e fixture make.
declare module "qrcode" {
  const QRCode: {
    toCanvas(
      canvas: HTMLCanvasElement,
      text: string,
      options: { errorCorrectionLevel?: string; width?: number },
      callback: (error: Error | null | undefined) => void,
    ): void;
    toDataURL(
      text: string,
      options?: {
        errorCorrectionLevel?: string;
        margin?: number;
        width?: number;
      },
    ): Promise<string>;
  };
  export default QRCode;
}

// @nuintun/qrcode ships JavaScript without declarations; these are the v5
// decoder entry points the import dialog calls.
declare module "@nuintun/qrcode" {
  export class BitMatrix {
    get width(): number;
    get height(): number;
  }
  export class Decoder {
    decode(matrix: BitMatrix): { content: string };
  }
  export class Detector {
    detect(matrix: BitMatrix): Generator<{ matrix: BitMatrix }, void, boolean>;
  }
  export function grayscale(imageData: ImageData): Uint8Array;
  export function binarize(
    luminances: Uint8Array,
    width: number,
    height: number,
  ): BitMatrix;
}

// highlight.js types its core but not the per-language modules
declare module "highlight.js/lib/languages/accesslog" {
  import type { LanguageFn } from "highlight.js";
  const language: LanguageFn;
  export default language;
}

// Markdown under src/docs is compiled to an HTML string by the vite plugin.
declare module "*.md" {
  const html: string;
  export default html;
}

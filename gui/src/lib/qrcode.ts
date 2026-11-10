import { binarize, Decoder, Detector, grayscale } from "@nuintun/qrcode";

/** Decodes the first QR code an image file contains. */
export async function decodeQrImage(file: Blob): Promise<string> {
  const bitmap = await createImageBitmap(file);
  try {
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) throw new Error("2d canvas context is unavailable");
    context.drawImage(bitmap, 0, 0);

    const luminances = grayscale(
      context.getImageData(0, 0, canvas.width, canvas.height),
    );
    const detector = new Detector();
    const decoder = new Decoder();
    const detected = detector.detect(
      binarize(luminances, canvas.width, canvas.height),
    );
    for (
      let current = detected.next();
      !current.done;
      current = detected.next(false)
    ) {
      try {
        return decoder.decode(current.value.matrix).content;
      } catch {
        // A detected region is not always a QR code; keep looking.
      }
    }
    throw new Error("no QR code found");
  } finally {
    bitmap.close();
  }
}

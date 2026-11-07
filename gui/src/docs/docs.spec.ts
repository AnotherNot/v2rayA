import { describe, expect, it } from "vitest";
import html from "../test-fixtures/raw-html.md";

// The docs compile to HTML at build time and the view inserts the result
// with v-html. A tag written in a Markdown file must reach the page as text,
// or a compromised docs file runs in the origin that holds the token.
describe("the docs Markdown compiler", () => {
  it("renders raw HTML as text", () => {
    expect(html).not.toContain("<script");
    expect(html).not.toContain("<img");
    expect(html).toContain("&lt;script&gt;");
    expect(html).toContain("&lt;img");
  });

  it("still renders ordinary Markdown", () => {
    expect(html).toContain("<strong>bold</strong>");
  });
});

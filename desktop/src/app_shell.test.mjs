import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const appShellHtml = readFileSync(resolve(import.meta.dirname, "../index.html"), "utf8");
const appStylesCss = readFileSync(resolve(import.meta.dirname, "./styles.css"), "utf8");

describe("app shell branding", () => {
  it("uses the shared Agent Flows mark instead of the legacy inline flame logo", () => {
    expect(appShellHtml).toContain('class="app-header-logo-mark"');
    expect(appShellHtml).toContain('class="hero-logo"');
    expect(appShellHtml).toContain('src="/tray-icon.png"');
    expect(appShellHtml).not.toContain(
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 71 48" fill="#FD4F00"',
    );
  });

  it("uses the indigo brand accent instead of the legacy orange theme", () => {
    expect(appStylesCss).toContain("--color-brand: #6366F1;");
    expect(appStylesCss).not.toContain("--color-brand: #FD4F00;");
  });
});

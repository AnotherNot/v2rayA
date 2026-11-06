// Starts the built service for the browser tests: a fresh configuration
// directory each run, so the first page is the first-run registration, and
// the binary embeds the interface the tests drive.
//
// `./build-all.sh` writes ./v2raya. The script fails with a pointer to it
// rather than serving a stale binary.
import { spawn } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..");
const binary = join(repo, "v2raya");
if (!existsSync(binary)) {
  console.error(
    `e2e: ${binary} is missing; run ./build-all.sh in ${repo} first`,
  );
  process.exit(1);
}

const state = join(here, ".state");
rmSync(state, { recursive: true, force: true });
mkdirSync(state, { recursive: true });

const address = `127.0.0.1:${process.env.V2RAYA_E2E_PORT ?? "21117"}`;
const child = spawn(binary, ["--lite", "--address", address], {
  cwd: repo,
  env: { ...process.env, XDG_CONFIG_HOME: state },
  stdio: "inherit",
});

// Playwright stops the server with SIGTERM; pass it on and stay until the
// service is gone, so the port is free for the next run.
for (const signal of ["SIGTERM", "SIGINT"]) {
  process.on(signal, () => child.kill(signal));
}
child.on("exit", (code, signal) => {
  process.exit(signal ? 0 : (code ?? 0));
});

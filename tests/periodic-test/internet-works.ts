import { Sandbox } from "@moru-ai/core";
import { DEBUG_TIMEOUT_MS, log, runTestWithSandbox } from "./utils.ts";

log("Starting sandbox logs test");

// Create sandbox
log("creating sandbox");
const sandbox = await Sandbox.create({ timeoutMs: DEBUG_TIMEOUT_MS });
log("ℹ️ sandbox created", sandbox.sandboxId);

await runTestWithSandbox(sandbox, "internet-works", async () => {
  const out = await sandbox.commands.run(
    "curl -s -o /dev/null -w '%{http_code}' https://www.gstatic.com/generate_204",
    {
      requestTimeoutMs: 10000,
    }
  );
  log("curl output", out.stdout);

  const internetWorking = out.stdout.trim() === "204";
  // verify internet is working
  if (!internetWorking) {
    log("Internet is not working, got status:", out.stdout);
    throw new Error("Internet is not working");
  }

  log("Test passed successfully");
});

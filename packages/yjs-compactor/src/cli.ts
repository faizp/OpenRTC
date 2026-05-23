import { pathToFileURL } from "node:url";
import { runCLI } from "./runner.ts";

if (process.argv[1] !== undefined && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    await runCLI(process.argv.slice(2));
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.error(JSON.stringify({ level: "error", msg: "compactor exited", error: message }));
    process.exit(1);
  }
}

export { runCLI };

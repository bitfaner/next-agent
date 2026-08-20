import { rmSync } from "node:fs";
import { homedir, platform, tmpdir } from "node:os";
import { dirname, join } from "node:path";

function defaultConfigDir() {
  if (process.platform === "win32") {
    return process.env.APPDATA ?? join(homedir(), "AppData", "Roaming");
  }
  if (process.platform === "darwin") {
    return join(homedir(), "Library", "Application Support");
  }
  return process.env.XDG_CONFIG_HOME ?? join(homedir(), ".config");
}

function defaultDbPath() {
  if (process.env.NEXTX_DB) {
    return process.env.NEXTX_DB;
  }
  const home = homedir();
  if (home) {
    return join(defaultConfigDir(), "nextx-agent", "nextx.db");
  }
  return join(tmpdir(), "nextx-agent.db");
}

const dbPath = defaultDbPath();
const paths = [dbPath, `${dbPath}-shm`, `${dbPath}-wal`];

for (const path of paths) {
  rmSync(path, { force: true });
}

console.log(`cleaned local sqlite data in ${dirname(dbPath)} (${platform()})`);

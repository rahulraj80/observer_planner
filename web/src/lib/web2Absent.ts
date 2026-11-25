// Fork change: the public snapshot (superbasedapp/observer) lacks web2/, the
// org dashboard. The web tests that read web2/ skip it there, a whole test or
// only its web2 rows, instead of failing; with web2/ present they run as
// upstream wrote them. Test support only: no app code imports this.
import { existsSync } from "node:fs";

export const WEB2_ABSENT = !existsSync(new URL("../../../web2/", import.meta.url));

// web2Skip is a node:test skip option for a test that reads path: the reason
// when path is under web2/ and the public snapshot lacks it, false otherwise.
export function web2Skip(path: string): string | false {
  return WEB2_ABSENT && path.startsWith("web2") ? "public snapshot lacks web2/" : false;
}

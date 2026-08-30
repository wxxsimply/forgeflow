import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../src/RunList.tsx", import.meta.url), "utf8");
if (!source.includes("RunList")) {
  throw new Error("RunList component is missing");
}
console.log("public web fixture passed");

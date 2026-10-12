// Minimal typing for the one Node import the token test needs; web/ has no @types/node.
declare module "node:fs" {
  export function readFileSync(path: string | URL, encoding: "utf8"): string;
}

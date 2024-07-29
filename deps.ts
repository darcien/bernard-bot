// This file will go away once we're fully migrated to import maps.
// See ./deno.jsonc for import maps.

// Importing from CDN resulting in empty object :(
// ```
// > import x from "https://esm.sh/e25n@1.1.0";
// undefined
// > x
// {}
// ```
// @deno-types="./e25n.d.ts"
export { default as e25n } from "https://esm.sh/e25n@1.1.0";
// export { default as e25n } from "https://cdn.skypack.dev/e25n@1.1.0";

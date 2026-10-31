// copy backend spec (monorepo root api/openapi.yaml) into web. backend = source of truth. never edit the copy.
import { copyFileSync, existsSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const src = resolve(root, '../api/openapi.yaml');
const dst = resolve(root, 'src/lib/api/openapi.yaml');

if (!existsSync(src)) {
  console.error(`spec not found: ${src}`);
  process.exit(1);
}
copyFileSync(src, dst);
console.log(`synced ${src} -> ${dst}`);

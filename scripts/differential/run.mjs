// Runs the Node reference checker of github.com/meaninggraph/core over every
// item of testdata/corpus and records its verdicts, with the core commit, in
// testdata/golden/node-verdicts.json. The Go tests compare `meaninggraph check`
// with that file; they never run Node themselves.
//
//   node scripts/differential/run.mjs <core checkout, npm ci done> <output.json>
//
// Use scripts/differential/regenerate.sh, which also checks that the checkout
// and the schema embedded in the binary agree.
import { execFileSync } from 'node:child_process';
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createChecker } from './node-checker.mjs';

const [, , coreArg, outArg] = process.argv;
if (!coreArg || !outArg) {
  console.error('usage: node scripts/differential/run.mjs <core checkout> <output.json>');
  process.exit(2);
}
const core = resolve(coreArg);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const corpus = join(repoRoot, 'testdata', 'corpus');
const checker = await createChecker(core, { graphRoot: repoRoot });
const version = (pkg) => JSON.parse(readFileSync(join(core, 'node_modules', pkg, 'package.json'), 'utf8')).version;

const status = execFileSync('git', ['-C', core, 'status', '--porcelain']).toString().trim();
if (status) {
  console.error(`the core checkout is not clean:\n${status}`);
  process.exit(2);
}
const commit = execFileSync('git', ['-C', core, 'rev-parse', 'HEAD']).toString().trim();

const items = {};
for (const name of readdirSync(corpus).sort()) {
  const dir = join(corpus, name);
  const item = JSON.parse(readFileSync(join(dir, 'item.json'), 'utf8'));
  items[name] = checker.verdictOf(item, dir);
}

writeFileSync(resolve(outArg), `${JSON.stringify({
  generated_by: 'scripts/differential/regenerate.sh',
  node_checker: {
    repository: 'https://github.com/meaninggraph/core',
    commit,
    node: process.version,
    ajv: version('ajv'),
    ajv_formats: version('ajv-formats'),
    yaml: version('yaml'),
  },
  items,
}, null, 2)}\n`);
console.log(`recorded ${Object.keys(items).length} verdicts of the reference checker at core ${commit}`);

// Random text-level mutants of the core and Chinook graphs, and of corpus items,
// each with the verdict of the Node reference checker. main.go then runs the
// Go library over the same directories and counts the disagreements by direction.
//
//   node mutate.mjs <core checkout> <chinook checkout> <count> <seed> <out dir>
//
// The mutations act on the text of the files (insert, delete, swap, break a
// line, add a tab, a carriage return, a quote, a ?, a tag ...), so most of what
// they find is YAML and HCL syntax. Same seed, same mutants.
import { cpSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createChecker } from '../node-checker.mjs';

const [, , coreArg, chinookArg, countArg, seedArg, outArg] = process.argv;
if (!coreArg || !chinookArg || !countArg || !seedArg || !outArg) {
  console.error('usage: node mutate.mjs <core checkout> <chinook checkout at the pinned commit> <count> <seed> <out dir>');
  process.exit(2);
}
const core = resolve(coreArg);
const chinook = resolve(chinookArg);
const out = resolve(outArg);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');
const corpus = join(repoRoot, 'testdata', 'corpus');
const CORE = 'github.com/meaninggraph/core';
const checker = await createChecker(core, { graphRoot: '/' });
// The parser the reference checker uses, to read the values of the mutated YAML
// files: main.go compares them with what ParseYAML reads from the same bytes.
const yaml = await import(pathToFileURL(join(core, 'node_modules', 'yaml', 'dist', 'index.js')).href);

let state = Number(seedArg) >>> 0 || 1;
const random = () => { state = (Math.imul(state, 1664525) + 1013904223) >>> 0; return state / 2 ** 32; };
const int = (n) => Math.floor(random() * n);
const pick = (list) => list[int(list.length)];

const yamlFragments = ['\t', ' ', '\n', '\r', '\r\n', '?', ': ', ':', '#', ' #c', '"', "'", '[', ']', '{', '}', ',', '&a ', '*a', '!!str ', '!!int ', '!!set ', '!x ', '|', '>', '-', '- ', '%YAML 1.2\n', '%TAG ! x\n', '---\n', '...\n', '\u0000', '\u0007', '\u007f', ' ', '\u0085', '﻿', '\\', '\\/', '\\u00e9', '\\uD83D\\uDE00', '<<: ', 'True', 'null', '~', '1e999', '0x1F', '.inf', 'é', '\u{1F600}', 'a: b\n', '? ', '?ref=abc', ', x?', ' ?', '|2', '>-', '+', "''", '""', '\\n', '9007199254740993', '1e3', '0o17', 'yes', 'No'];
const hclFragments = ['{', '}', '[', ']', '=', ',', '"', '\\', '${x}', '#', '//', '/*', '*/', '\n', '\t', ' ', 'true', 'false', '5', '-1', '1.5', 'entity "X" {\n}\n', 'property "p" {\n}\n', 'key = ["Id"]', 'key = "Id"', 'type = "int"', 'entity = "Artist"', 'entity = true', 'constructor', 'toString', '__proto__', 'component "C" {\n}\n', 'enum "E" {\n}\n', 'use = ["C"]', 'values = ["a"]'];

function mutateText(text, fragments) {
  let t = text;
  const lines = () => t.split('\n');
  const ops = [
    () => { const i = int(t.length + 1); t = t.slice(0, i) + pick(fragments) + t.slice(i); },
    () => { const i = int(t.length); t = t.slice(0, i) + t.slice(i + 1 + int(3)); },
    () => { const l = lines(); l.splice(int(l.length), 1); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = pick(['\t', ' ', '  ', '\t# x']) + l[i]; t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] += pick([' #x', '#x', '\t', ' ', ',', ':', '?', ' \\']); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); const j = int(l.length); [l[i], l[j]] = [l[j], l[i]]; t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l.splice(int(l.length), 0, l[i]); t = l.join('\n'); },
    () => { const i = int(t.length); if (t[i]) t = t.slice(0, i) + pick(['"', "'", '[', ']', '{', '}', ':', '-', '?', '#', ',', '|', '>', '*', '&', '!', '%', '@', '`', '=', '\\']) + t.slice(i + 1); },
    () => { t = t.replace(/\[([^\]\n]*)\]/, (m, a) => `[${a}${pick(['?ref=abc', ', x?', ' ?', ',', ' # c'])}]`); },
    () => { t = t.replace(/: ([A-Za-z][^\n]*)\n/, (m, a) => `: ${pick(['"', "'"])}${a}${pick(['"', "'"])}${pick(['', '', '#x', ' #x', ' x'])}\n`); },
    () => { t = t.replace(/\n/, pick(['\r\n', '\r', '\n\n', '\n\t\n', '\n  \n'])); },
    () => { t = t.replace(/\n/g, pick(['\n', '\n', '\r\n'])); },
  ];
  for (let n = 1 + int(3); n > 0; n--) pick(ops)();
  return t;
}

const read = (path) => readFileSync(path, 'utf8');
const coreFiles = readdirSync(core).filter((n) => n.endsWith('.meaning.yaml')).sort();
const chinookMeaning = read(join(chinook, 'model/chinook.meaning.yaml'));
const chinookModel = read(join(chinook, 'model/chinook.modelspec.hcl'));
const corpusItems = readdirSync(corpus).sort();

rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, 'variants'), { recursive: true });
const verdicts = {};
const values = {}; // variant -> { mutated YAML file (relative) -> the value Node reads, when it reads one }
const count = Number(countArg);
for (let i = 0; i < count; i++) {
  const name = `v${i}`;
  const dir = join(out, 'variants', name);
  mkdirSync(dir);
  let item;
  let mutatedYAML = null;
  switch (i % 4) {
    case 0: { // a core file mutated, the others intact; checked as core itself
      const target = pick(coreFiles);
      for (const file of coreFiles) writeFileSync(join(dir, file), file === target ? mutateText(read(join(core, file)), yamlFragments) : read(join(core, file)));
      mutatedYAML = target;
      item = { address: CORE };
      break;
    }
    case 1: { // the Chinook meaning file mutated, with its model, extending the pristine core
      mkdirSync(join(dir, 'model'));
      writeFileSync(join(dir, 'model', 'chinook.meaning.yaml'), mutateText(chinookMeaning, yamlFragments));
      mutatedYAML = 'model/chinook.meaning.yaml';
      writeFileSync(join(dir, 'model', 'chinook.modelspec.hcl'), chinookModel);
      item = { address: 'github.com/datatug/chinookdb', check: 'model', graphs: { [CORE]: core } };
      break;
    }
    case 2: { // the Chinook model mutated
      mkdirSync(join(dir, 'model'));
      writeFileSync(join(dir, 'model', 'chinook.meaning.yaml'), chinookMeaning);
      writeFileSync(join(dir, 'model', 'chinook.modelspec.hcl'), mutateText(chinookModel, hclFragments));
      item = { address: 'github.com/datatug/chinookdb', check: 'model', graphs: { [CORE]: core } };
      break;
    }
    default: { // a corpus item with one of its files mutated
      const source = join(corpus, pick(corpusItems));
      cpSync(source, dir, { recursive: true });
      const original = JSON.parse(read(join(source, 'item.json')));
      item = { ...original, graphs: Object.fromEntries(Object.entries(original.graphs ?? {}).map(([a, p]) => [a, resolve(repoRoot, p)])) };
      const candidates = [];
      const walk = (d) => { for (const e of readdirSync(d, { withFileTypes: true })) { const p = join(d, e.name); if (e.isDirectory()) walk(p); else if (e.name !== 'item.json' && (e.name.endsWith('.yaml') || e.name.endsWith('.hcl'))) candidates.push(p); } };
      walk(dir);
      if (candidates.length) {
        const file = pick(candidates);
        writeFileSync(file, mutateText(read(file), file.endsWith('.hcl') ? hclFragments : yamlFragments));
        if (file.endsWith('.yaml')) mutatedYAML = file.slice(dir.length + 1);
      }
    }
  }
  writeFileSync(join(dir, 'item.json'), JSON.stringify(item));
  verdicts[name] = checker.verdictOf(item, dir);
  if (mutatedYAML) {
    try {
      const doc = yaml.parseDocument(read(join(dir, mutatedYAML)));
      if (doc.errors.length === 0) values[name] = { [mutatedYAML]: doc.toJS() ?? null };
    } catch { /* not read: nothing to compare */ }
  }
}
writeFileSync(join(out, 'verdicts.json'), JSON.stringify(verdicts));
writeFileSync(join(out, 'values.json'), JSON.stringify(values));
const accepted = Object.values(verdicts).filter((v) => v.verdict === 'accept').length;
console.log(`${count} mutants of seed ${seedArg}: the reference checker accepts ${accepted} and refuses ${count - accepted}`);

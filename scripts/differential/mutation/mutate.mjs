// Random text-level mutants of the core and Chinook graphs, and of corpus items,
// each with the verdict of the Node reference checker. main.go then runs the
// Go library over the same directories and counts the disagreements by direction.
//
//   node mutate.mjs <core checkout> <chinook checkout> <count> <seed> <out dir>
//
// The mutations act on the text of the files (insert, delete, swap, break a
// line, add a tab, a carriage return, a quote, a ?, a tag ...), so most of what
// they find is YAML and HCL syntax. Beside the random text mutations, three
// families are generated on purpose, because random mutation does not reach
// them: the ends of block scalars at the end of a file (every chomping style,
// every kind of last line: spaces, tabs, comments, line ends), byte order marks
// before every kind of first line, tabs in every position, and the names of
// JavaScript's Object.prototype in every name position of a model (block names,
// attribute names, bindings), and comment lines and blank lines (with and
// without tabs, at every column, `#x` and `# x`) in every place of a file: between a
// key or a dash and its value, after a block scalar header, inside a multi-line
// plain scalar, as the last line with and without a line break, keys at the
// limit of 1024 as written (plain, quoted, with escapes, doubled quotes, multibyte
// text, blanks before the colon). Same seed, same mutants.
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

const protoNames = ['__defineGetter__', '__defineSetter__', '__lookupGetter__', '__lookupSetter__', '__proto__', 'constructor', 'hasOwnProperty', 'isPrototypeOf', 'propertyIsEnumerable', 'toLocaleString', 'toString', 'valueOf'];

// protoModel puts a name of Object.prototype into the model in a name position:
// the name of an entity, property, component, enum or field, an attribute name in
// any block (also a new attribute), or, in the meaning file, in a binding.
function protoModel(hcl, meaning) {
  let model = hcl;
  let bindings = meaning;
  const name = () => pick(protoNames);
  const ops = [
    () => { const all = [...model.matchAll(/\b(entity|property|component|enum|field)\s+"([^"]+)"/g)]; if (all.length) { const m = pick(all); model = model.slice(0, m.index) + m[0].replace(/"[^"]+"/, `"${name()}"`) + model.slice(m.index + m[0].length); } },
    () => { const all = [...model.matchAll(/^(\s*)(key|type|entity|required|use|values|enum|component)(\s*=)/gm)]; if (all.length) { const m = pick(all); model = model.slice(0, m.index) + `${m[1]}${name()}${m[3]}` + model.slice(m.index + m[0].length); } },
    () => { const all = [...model.matchAll(/\{[ \t]*\n/g)]; if (all.length) { const m = pick(all); model = model.slice(0, m.index + m[0].length) + `  ${name()} = ${pick(['1', 'true', '"x"', '["a"]'])}\n` + model.slice(m.index + m[0].length); } },
    () => { const all = [...model.matchAll(/\{([^{}\n]*)\}/g)].filter((m) => m[1].includes('=')); if (all.length) { const m = pick(all); model = model.slice(0, m.index) + `{ ${name()} = 1, ${m[1].trim()} }`.replace(', ', '\n') + model.slice(m.index + m[0].length); } },
    () => { bindings = bindings.replace(/chinook\.[A-Za-z]+/, `chinook.${name()}`); },
    () => { bindings = bindings.replace(/property: [A-Za-z]+/, `property: ${name()}`); },
  ];
  for (let n = 1 + int(2); n > 0; n--) pick(ops)();
  return { model, meaning: bindings };
}

const blockStyles = ['|', '>', '|-', '>-', '|+', '>+', '|2', '>-2', '|+2', '|2-', '|1', '>2'];
const blockLines = ['', 'text', 'more words here', ' indented', '  more indented', '# looks like a comment', 'x   ', 'a: b', '- not a list', '"quoted"', '\ttab inside', 'tab\tinside', ' \ttab after space'];
// An ending for the file after the last line of the scalar: whitespace and
// comments of every kind.
function blockEnding(indent) {
  const spaces = (k) => ' '.repeat(k);
  return pick(['', '\n', '\n\n', '\n\n\n', `\n${spaces(indent)}`, `\n${spaces(indent + 1 + int(4))}`, `\n${spaces(int(indent))}`, `\n\n${spaces(int(indent + 4))}`, `\n${spaces(int(indent + 4))}\n`, '\n\t', '\n\t\n', `\n${spaces(indent)}\t`, `\n${spaces(indent)}\t\n`, '\n# end', `\n${spaces(indent)}# end`, '\n# end\n', '\r\n', `\r\n${spaces(indent + 2)}`, `\r\n${spaces(indent)}`, '\n\r\n', ' ', '\t', '\n  ', '\n    ', '\n      ', '\n        ']);
}
const conceptHead = 'format: meaning/draft-1\nid: demo\nname: Demo\ndescription: A demo graph.\nconcepts:\n';
function blockScalarFile() {
  const key = pick(['description', 'description', 'name']);
  const indent = pick([6, 6, 8, 5, 4 + 2]);
  const lines = Array.from({ length: 1 + int(4) }, () => pick(blockLines));
  const body = lines.map((l) => (l === '' ? '' : `${' '.repeat(indent)}${l}`)).join('\n');
  const scalar = `    ${key === 'name' ? 'description' : key}: ${pick(blockStyles)}${pick(['', '', '', ' # c', '\t'])}\n${body}`;
  const first = `  - id: a\n    kind: entity\n    labels: {en: a}\n`;
  const next = pick(['', '', `\n  - id: b\n    kind: entity\n    labels: {en: b}\n    description: d`]);
  return conceptHead + first + scalar + blockEnding(indent) + next;
}

// bomFile puts a byte order mark before the first line of a document, which may
// have been indented, started by a comment, a blank line or ---.
function bomFile(text) {
  const shift = pick([0, 0, 1, 2, 3]);
  const lines = text.split('\n').map((l) => (shift && l ? ' '.repeat(shift) + l : l));
  const first = pick(['', '', '', '# c\n', '  # c\n', '\n', '---\n', '  \n', '\t# c\n', '- a\n']);
  return pick(['\ufeff', '\ufeff', '\ufeff', '']) + first + lines.join('\n');
}

// tabFile puts tabs into a document: in indentation, after a colon or a dash,
// at the ends of lines, before comments, in values, on blank lines.
function tabFile(text) {
  let t = text;
  const lines = () => t.split('\n');
  const ops = [
    () => { const l = lines(); const i = int(l.length); l[i] = '\t' + l[i]; t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/^( *)/, (m) => m.slice(0, int(m.length + 1)) + '\t' + m.slice(0, 0)); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] += '\t'.repeat(1 + int(2)); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/: /, ':\t'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/- /, '-\t'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/ /, '\t'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/ #/, '\t#'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/([a-z])([a-z])/, '$1\t$2'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l.splice(i, 0, pick(['\t', ' \t', '\t ', '\t# c', '  \t# c', '\t\t'])); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/\{ ?/, (m) => m + '\t'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/,/, ',\t'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/(")([^"]+)(")/, '$1\t$2\t$3'); t = l.join('\n'); },
    () => { const l = lines(); const i = int(l.length); l[i] = l[i].replace(/\]/, '\t]'); t = l.join('\n'); },
  ];
  for (let n = 1 + int(3); n > 0; n--) pick(ops)();
  return t;
}

const fillerLines = [];
for (const col of [0, 1, 2, 3, 4, 6, 8]) for (const text of ['#x', '# x', '#']) fillerLines.push(' '.repeat(col) + text);
fillerLines.push('#TODO reword', '# TODO reword', '', '  ', '    ', '\t#c', '\t# c', '  \t# c', '# a\tb', '#\tc', '\t', ' \t', '  \t ', '\t  ');
// fillerFile puts one to three comment or blank lines between the lines of a file,
// anywhere, the last of them possibly the last line of the file, with or without
// a line break after it.
function fillerFile(text) {
  const lines = text.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  let endsWithBreak = true;
  for (let n = 1 + int(3); n > 0; n--) {
    const at = int(lines.length + 1);
    lines.splice(at, 0, pick(fillerLines));
    if (at === lines.length - 1) endsWithBreak = int(2) === 0;
  }
  return lines.join('\n') + (endsWithBreak ? '\n' : '');
}
// The shapes where the reference parser reads a comment line in a way a reader
// that skips comments does not: a value on a later line, in a concept.
const conceptShapes = [
  'description:\n      A thing.\n', 'synonyms:\n      en:\n        -\n          item\n        - object\n    description: A thing.\n',
  'description: |\n      A thing.\n', 'description: >-\n      A\n      thing.\n    synonyms: {en: [item]}\n', 'description: A\n      thing.\n',
  'synonyms:\n      en:\n        - one\n        - two\n    description: A thing.\n', 'description: | # c\n      text\n    synonyms: {en: [x]}\n', 'description:\n    # c\n      A thing.\n',
];
const shapeFile = () => 'format: meaning/draft-1\nid: demo\nname: Demo\ndescription: A demo graph.\nconcepts:\n  - id: thing\n    kind: entity\n    labels: {en: thing}\n    ' + pick(conceptShapes);

// keyFile writes a key of a mapping whose length as written is near the limit of
// 1024: the reference parser counts the source from the first character of a
// block mapping key to its colon, in UTF-16 units, so quotes, escapes as written
// and blanks before the colon count. The key is plain, double-quoted (with
// escapes, which are longer written than read) or single-quoted (with doubled
// quotes), made of ASCII letters (a key a schema accepts as a code) or of
// multibyte characters, and stands among the codes of a value or at the root.
function keyFile() {
  const styles = {
    'plain': () => 'k', 'plain-2-bytes': () => '\u00e9', 'plain-3-bytes': () => '\u20ac', 'plain-astral': () => '\u{1F600}',
    'dq': () => 'k', 'dq-multibyte': () => '\u00e9', 'dq-escapes': () => pick(['\\u006b', '\\x6b', 'k']), 'dq-quoted': () => '\\"',
    'sq': () => 'k', 'sq-doubled': () => "''",
  };
  const style = pick(Object.keys(styles));
  const aim = 1010 + int(30);
  let body = '';
  while (body.length < aim) body += styles[style]();
  const quote = style.startsWith('dq') ? '"' : style.startsWith('sq') ? "'" : '';
  const key = quote + body + quote + ' '.repeat(pick([0, 0, 1, 2, 3]));
  const head = 'format: meaning/draft-1\nid: demo\nname: Demo\ndescription: A demo graph.\nconcepts:\n  - id: currency\n    kind: entity\n    labels: {en: currency}\n    description: A currency.\n    values:\n      - id: usd\n        labels: {en: US dollar}\n        codes:\n          iso: USD\n';
  return int(4) === 0 ? `${key}: X\n${head}` : `${head}          ${key}: X\n`;
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
  switch (i % 11) {
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
    case 3: { // the names of Object.prototype in every name position of the Chinook model and its bindings
      mkdirSync(join(dir, 'model'));
      const changed = protoModel(chinookModel, chinookMeaning);
      writeFileSync(join(dir, 'model', 'chinook.meaning.yaml'), changed.meaning);
      writeFileSync(join(dir, 'model', 'chinook.modelspec.hcl'), changed.model);
      mutatedYAML = 'model/chinook.meaning.yaml';
      item = { address: 'github.com/datatug/chinookdb', check: 'model', graphs: { [CORE]: core } };
      break;
    }
    case 4: { // a block scalar at the end of a file, in every style and with every ending
      writeFileSync(join(dir, 'demo.meaning.yaml'), blockScalarFile());
      mutatedYAML = 'demo.meaning.yaml';
      item = {};
      break;
    }
    case 5: { // a byte order mark, and a first line of every kind, before a core file, the Chinook file or a block scalar file
      const choice = int(3);
      if (choice === 0) {
        const target = pick(coreFiles);
        for (const file of coreFiles) writeFileSync(join(dir, file), file === target ? bomFile(read(join(core, file))) : read(join(core, file)));
        mutatedYAML = target;
        item = { address: CORE };
      } else if (choice === 1) {
        mkdirSync(join(dir, 'model'));
        writeFileSync(join(dir, 'model', 'chinook.meaning.yaml'), bomFile(chinookMeaning));
        writeFileSync(join(dir, 'model', 'chinook.modelspec.hcl'), chinookModel);
        mutatedYAML = 'model/chinook.meaning.yaml';
        item = { address: 'github.com/datatug/chinookdb', check: 'model', graphs: { [CORE]: core } };
      } else {
        writeFileSync(join(dir, 'demo.meaning.yaml'), bomFile(blockScalarFile()));
        mutatedYAML = 'demo.meaning.yaml';
        item = {};
      }
      break;
    }
    case 6: { // tabs in every position of a core file, the Chinook file or a block scalar file
      const choice = int(3);
      if (choice === 0) {
        const target = pick(coreFiles);
        for (const file of coreFiles) writeFileSync(join(dir, file), file === target ? tabFile(read(join(core, file))) : read(join(core, file)));
        mutatedYAML = target;
        item = { address: CORE };
      } else if (choice === 1) {
        mkdirSync(join(dir, 'model'));
        writeFileSync(join(dir, 'model', 'chinook.meaning.yaml'), tabFile(chinookMeaning));
        writeFileSync(join(dir, 'model', 'chinook.modelspec.hcl'), chinookModel);
        mutatedYAML = 'model/chinook.meaning.yaml';
        item = { address: 'github.com/datatug/chinookdb', check: 'model', graphs: { [CORE]: core } };
      } else {
        writeFileSync(join(dir, 'demo.meaning.yaml'), tabFile(blockScalarFile()));
        mutatedYAML = 'demo.meaning.yaml';
        item = {};
      }
      break;
    }
    case 8: { // comment and blank lines anywhere in a core file or the Chinook file
      if (int(2) === 0) {
        const target = pick(coreFiles);
        for (const file of coreFiles) writeFileSync(join(dir, file), file === target ? fillerFile(read(join(core, file))) : read(join(core, file)));
        mutatedYAML = target;
        item = { address: CORE };
      } else {
        mkdirSync(join(dir, 'model'));
        writeFileSync(join(dir, 'model', 'chinook.meaning.yaml'), fillerFile(chinookMeaning));
        writeFileSync(join(dir, 'model', 'chinook.modelspec.hcl'), chinookModel);
        mutatedYAML = 'model/chinook.meaning.yaml';
        item = { address: 'github.com/datatug/chinookdb', check: 'model', graphs: { [CORE]: core } };
      }
      break;
    }
    case 9: { // comment and blank lines in the shapes that read differently: a value on a later line
      writeFileSync(join(dir, 'demo.meaning.yaml'), fillerFile(shapeFile()));
      mutatedYAML = 'demo.meaning.yaml';
      item = {};
      break;
    }
    case 10: { // a key of a mapping near the limit of 1024 as written
      writeFileSync(join(dir, 'demo.meaning.yaml'), keyFile());
      mutatedYAML = 'demo.meaning.yaml';
      item = {};
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

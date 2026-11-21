#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const [name, summaryArg] = process.argv.slice(2);
if (!name || !summaryArg) {
  console.error('usage: check-js-coverage.mjs <project> <coverage-summary.json>');
  process.exit(2);
}

const thresholds = JSON.parse(fs.readFileSync(path.join(root, 'quality/js-coverage-thresholds.json'), 'utf8'));
const expected = thresholds[name];
if (!expected) {
  console.error(`No JavaScript coverage thresholds configured for ${name}`);
  process.exit(2);
}

const summaryPath = path.resolve(summaryArg);
const summary = JSON.parse(fs.readFileSync(summaryPath, 'utf8'));
const total = summary.total;
let failed = false;
for (const metric of ['lines', 'statements', 'functions', 'branches']) {
  const actual = Number(total?.[metric]?.pct);
  const minimum = Number(expected[metric]);
  if (!Number.isFinite(actual) || !Number.isFinite(minimum)) {
    console.error(`${name}: invalid ${metric} coverage data`);
    failed = true;
    continue;
  }
  console.log(`${name} ${metric}: ${actual.toFixed(2)}% (minimum ${minimum.toFixed(2)}%)`);
  if (actual + 0.0001 < minimum) failed = true;
}
if (failed) process.exit(1);

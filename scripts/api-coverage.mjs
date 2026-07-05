#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import ts from "typescript";

const repoRoot = path.resolve(new URL("..", import.meta.url).pathname);
const threshold = Number.parseFloat(process.argv[2] ?? process.env.OPENRTC_TS_EXPORT_COVERAGE_MIN ?? "90");

if (!Number.isFinite(threshold) || threshold < 0 || threshold > 100) {
  console.error("TypeScript export coverage threshold must be a number between 0 and 100");
  process.exit(2);
}

const packageRoot = path.join(repoRoot, "packages");
const packageNames = fs.readdirSync(packageRoot).filter((name) => {
  return fs.statSync(path.join(packageRoot, name)).isDirectory();
});

let total = 0;
let covered = 0;
const packageResults = [];

for (const packageName of packageNames.sort()) {
  const packageDir = path.join(packageRoot, packageName);
  const packageJSONPath = path.join(packageDir, "package.json");
  if (!fs.existsSync(packageJSONPath)) {
    continue;
  }
  const packageJSON = JSON.parse(fs.readFileSync(packageJSONPath, "utf8"));
  const entrypoints = packageEntrypoints(packageJSON.exports);
  if (entrypoints.length === 0) {
    continue;
  }

  const publicValueExports = new Set();
  for (const entrypoint of entrypoints) {
    const sourcePath = path.join(packageDir, entrypoint.replace(/^\.\//, ""));
    for (const name of exportedValueNames(sourcePath)) {
      publicValueExports.add(name);
    }
  }

  const testText = packageTestText(packageDir);
  const missing = [];
  let packageCovered = 0;
  for (const name of [...publicValueExports].sort()) {
    if (new RegExp(`\\b${escapeRegExp(name)}\\b`).test(testText)) {
      packageCovered += 1;
    } else {
      missing.push(name);
    }
  }

  const packageTotal = publicValueExports.size;
  total += packageTotal;
  covered += packageCovered;
  packageResults.push({
    name: packageName,
    covered: packageCovered,
    total: packageTotal,
    missing,
  });
}

const percent = total === 0 ? 100 : (covered / total) * 100;
for (const result of packageResults) {
  const packagePercent = result.total === 0 ? 100 : (result.covered / result.total) * 100;
  const suffix = result.missing.length > 0 ? ` missing: ${result.missing.join(", ")}` : "";
  console.log(`${result.name}: ${result.covered}/${result.total} public value exports referenced (${packagePercent.toFixed(1)}%)${suffix}`);
}
console.log(`TypeScript public value export coverage: ${covered}/${total} (${percent.toFixed(1)}%), threshold ${threshold.toFixed(1)}%`);

if (percent + Number.EPSILON < threshold) {
  process.exit(1);
}

function packageEntrypoints(exportsField) {
  if (typeof exportsField === "string") {
    return [exportsField];
  }
  if (!exportsField || typeof exportsField !== "object") {
    return [];
  }
  const entrypoints = [];
  for (const value of Object.values(exportsField)) {
    if (typeof value === "string") {
      entrypoints.push(value);
    } else if (value && typeof value === "object") {
      for (const nested of Object.values(value)) {
        if (typeof nested === "string") {
          entrypoints.push(nested);
        }
      }
    }
  }
  return [...new Set(entrypoints)];
}

function exportedValueNames(sourcePath) {
  const source = fs.readFileSync(sourcePath, "utf8");
  const file = ts.createSourceFile(sourcePath, source, ts.ScriptTarget.Latest, true);
  const names = new Set();

  for (const statement of file.statements) {
    if (
      (ts.isFunctionDeclaration(statement) ||
        ts.isClassDeclaration(statement) ||
        ts.isEnumDeclaration(statement) ||
        ts.isModuleDeclaration(statement)) &&
      hasExportModifier(statement) &&
      statement.name
    ) {
      names.add(statement.name.text);
    }

    if (ts.isVariableStatement(statement) && hasExportModifier(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        if (ts.isIdentifier(declaration.name)) {
          names.add(declaration.name.text);
        }
      }
    }

    if (
      ts.isExportDeclaration(statement) &&
      !statement.isTypeOnly &&
      statement.exportClause &&
      ts.isNamedExports(statement.exportClause)
    ) {
      for (const element of statement.exportClause.elements) {
        names.add(element.name.text);
      }
    }
  }

  return [...names].filter((name) => !name.startsWith("_"));
}

function hasExportModifier(node) {
  return node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword) ?? false;
}

function packageTestText(packageDir) {
  const srcDir = path.join(packageDir, "src");
  if (!fs.existsSync(srcDir)) {
    return "";
  }
  return fs
    .readdirSync(srcDir)
    .filter((name) => /\.(test|typecheck)\.ts$/.test(name))
    .map((name) => fs.readFileSync(path.join(srcDir, name), "utf8"))
    .join("\n");
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

import ts from "typescript";
import { realpathSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { SourceRef } from "@cms/manifest";
import type { Registry } from "./registry.js";

export interface CodeElement {
  definition: { file: string; line: number };
  implementation: SourceRef;
  dependencies: string[];
  tests: string[];
}
export interface CodeIndex {
  repo: { root: "."; commit: string };
  elements: Record<string, CodeElement>;
  conventions: { registry: string; testCommand: string; manifestCommand: string };
}
export interface SourceIndexResult {
  index: CodeIndex;
  refs: Record<string, SourceRef>;
}
export function projectProgram(configFile: string): ts.Program {
  const loaded = ts.readConfigFile(configFile, ts.sys.readFile);
  if (loaded.error) throw new Error("CMS_TSCONFIG_INVALID");
  const config = ts.parseJsonConfigFileContent(loaded.config, ts.sys, resolve(configFile, ".."));
  if (config.errors.length) throw new Error("CMS_TSCONFIG_INVALID");
  const program = ts.createProgram({
    rootNames: config.fileNames,
    options: { ...config.options, noEmit: true },
  });
  const errors = ts
    .getPreEmitDiagnostics(program)
    .filter((item) => item.category === ts.DiagnosticCategory.Error);
  if (errors.length)
    throw new Error(
      "CMS_TYPECHECK_FAILED: " +
        errors
          .map(
            (item) =>
              "TS" +
              item.code +
              (item.file
                ? " " +
                  relative(resolve(configFile, ".."), item.file.fileName).replaceAll("\\", "/") +
                  ":" +
                  (item.file.getLineAndCharacterOfPosition(item.start ?? 0).line + 1)
                : ""),
          )
          .join("; "),
    );
  return program;
}
export function sourceIndex(
  program: ts.Program,
  registryFile: string,
  registry: Registry,
  root: string,
  commit: string,
): SourceIndexResult {
  const checker = program.getTypeChecker();
  const packageRoot = resolve(fileURLToPath(import.meta.url), "../..");
  const factoryFiles = new Set(
    [
      resolve(packageRoot, "src/registry.ts"),
      resolve(packageRoot, "dist/registry.d.ts"),
      resolve(packageRoot, "src/operations.ts"),
      resolve(packageRoot, "dist/operations.d.ts"),
      resolve(packageRoot, "../react/src/registry.ts"),
      resolve(packageRoot, "../react/dist/registry.d.ts"),
    ].map((path) => path.replaceAll("\\", "/")),
  );
  const realRoot = realpathSync(root);
  const fileName = (file: ts.SourceFile): string | undefined => {
    if (file.isDeclarationFile) return undefined;
    const path = relative(realRoot, realpathSync(file.fileName)).replaceAll("\\", "/");
    if (
      !path ||
      path === ".." ||
      path.startsWith("../") ||
      isAbsolute(path) ||
      path.split("/").some((part) => ["node_modules", "generated", "dist"].includes(part))
    )
      return undefined;
    return path;
  };
  const imports = (file: ts.SourceFile): ts.SourceFile[] => {
    const files: ts.SourceFile[] = [];
    const collect = (node: ts.Node) => {
      let specifier: ts.Expression | undefined;
      if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node))
        specifier = node.moduleSpecifier;
      else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword)
        specifier = node.arguments[0];
      if (specifier && ts.isStringLiteral(specifier)) {
        const module = ts.resolveModuleName(
          specifier.text,
          file.fileName,
          program.getCompilerOptions(),
          ts.sys,
        ).resolvedModule;
        const target = module && program.getSourceFile(module.resolvedFileName);
        if (target && fileName(target)) files.push(target);
      }
      ts.forEachChild(node, collect);
    };
    collect(file);
    return [...new Set(files)];
  };
  const entry = program.getSourceFile(resolve(registryFile));
  if (!entry || !fileName(entry)) throw new Error("CMS_REGISTRY_SOURCE_MISSING");
  // Resolve framework helpers in the application's dependency layout as well.
  for (const name of ["@cms/component-registry", "@cms/react"]) {
    const module = ts.resolveModuleName(
      name,
      entry.fileName,
      program.getCompilerOptions(),
      ts.sys,
    ).resolvedModule;
    if (module)
      for (const file of ["registry.ts", "registry.d.ts", "operations.ts", "operations.d.ts"])
        factoryFiles.add(resolve(dirname(module.resolvedFileName), file).replaceAll("\\", "/"));
  }
  const reachable = new Set<ts.SourceFile>();
  const visit = (file: ts.SourceFile) => {
    if (reachable.has(file)) return;
    if (reachable.size >= 512) throw new Error("CMS_SOURCE_GRAPH_LIMIT");
    reachable.add(file);
    for (const imported of imports(file)) visit(imported);
  };
  visit(entry);
  const alias = (symbol: ts.Symbol): ts.Symbol => {
    const seen = new Set<ts.Symbol>();
    while (symbol.flags & ts.SymbolFlags.Alias) {
      if (seen.has(symbol)) throw new Error("CMS_SOURCE_ALIAS_CYCLE");
      seen.add(symbol);
      symbol = checker.getAliasedSymbol(symbol);
    }
    return symbol;
  };
  const calls = new Map<string, { call: ts.CallExpression; implementation: ts.Expression }[]>();
  for (const file of reachable) {
    if (/\.(test|spec)\.[cm]?[jt]sx?$/.test(file.fileName)) continue;
    const scan = (node: ts.Node) => {
      if (ts.isCallExpression(node)) {
        const symbol = checker.getSymbolAtLocation(node.expression);
        const factory = symbol && alias(symbol);
        const name = factory?.getName();
        if (
          ["defineComponent", "defineReactComponent", "defineAction", "defineDataSource"].includes(
            name ?? "",
          ) &&
          factory!
            .getDeclarations()
            ?.some((declaration) =>
              factoryFiles.has(
                realpathSync(declaration.getSourceFile().fileName).replaceAll("\\", "/"),
              ),
            )
        ) {
          const argument = node.arguments[0];
          if (!argument || !ts.isObjectLiteralExpression(argument))
            throw new Error("CMS_SOURCE_DEFINITION_UNSUPPORTED");
          let componentName: string | undefined;
          let implementation: ts.Expression | undefined;
          const kind =
            name === "defineAction"
              ? "action"
              : name === "defineDataSource"
                ? "dataSource"
                : "component";
          const implementationKey =
            kind === "action" ? "handler" : kind === "dataSource" ? "loader" : "component";
          for (const property of argument.properties) {
            if (
              ts.isPropertyAssignment(property) &&
              (ts.isIdentifier(property.name) || ts.isStringLiteral(property.name))
            ) {
              if (
                property.name.text === "name" &&
                (ts.isStringLiteral(property.initializer) ||
                  ts.isNoSubstitutionTemplateLiteral(property.initializer))
              )
                componentName = property.initializer.text;
              if (property.name.text === implementationKey) implementation = property.initializer;
            } else if (
              ts.isShorthandPropertyAssignment(property) &&
              property.name.text === implementationKey
            )
              implementation = property.name;
          }
          if (!componentName || !implementation)
            throw new Error("CMS_SOURCE_DEFINITION_UNSUPPORTED");
          const key = kind + ":" + componentName;
          const records = calls.get(key) ?? [];
          records.push({ call: node, implementation });
          calls.set(key, records);
        }
      }
      ts.forEachChild(node, scan);
    };
    scan(file);
  }
  const refs: Record<string, SourceRef> = Object.create(null);
  const elements: Record<string, CodeElement> = Object.create(null);
  const definitions = [
    ...registry.components.map((definition) => ({ definition, kind: "component" })),
    ...registry.actions.map((definition) => ({ definition, kind: "action" })),
    ...registry.dataSources.map((definition) => ({ definition, kind: "dataSource" })),
  ];
  for (const { definition, kind } of definitions) {
    const candidates = calls.get(kind + ":" + definition.name);
    if (!candidates || candidates.length !== 1)
      throw new Error("CMS_SOURCE_DEFINITION_AMBIGUOUS: " + definition.name);
    const candidate = candidates[0]!;
    const symbol = ts.isShorthandPropertyAssignment(candidate.implementation.parent)
      ? checker.getShorthandAssignmentValueSymbol(candidate.implementation.parent)
      : checker.getSymbolAtLocation(candidate.implementation);
    if (!symbol) throw new Error("CMS_SOURCE_IMPLEMENTATION_UNRESOLVED: " + definition.name);
    const target = alias(symbol);
    const declarations = target
      .getDeclarations()
      ?.filter((node) => !node.getSourceFile().isDeclarationFile);
    if (!declarations?.length)
      throw new Error("CMS_SOURCE_IMPLEMENTATION_UNRESOLVED: " + definition.name);
    const implementation = declarations[0]!;
    const implementationFile = implementation.getSourceFile();
    const implementationPath = fileName(implementationFile);
    if (!implementationPath)
      throw new Error("CMS_SOURCE_IMPLEMENTATION_EXTERNAL: " + definition.name);
    const module = checker.getSymbolAtLocation(implementationFile);
    const exported =
      module && checker.getExportsOfModule(module).filter((item) => alias(item) === target);
    if (!exported?.length)
      throw new Error("CMS_SOURCE_IMPLEMENTATION_NOT_EXPORTED: " + definition.name);
    const ref = {
      file: implementationPath,
      export: exported.map((item) => item.getName()).sort()[0]!,
      line: implementationFile.getLineAndCharacterOfPosition(implementation.getStart()).line + 1,
    };
    refs[kind === "component" ? definition.name : kind + ":" + definition.name] = ref;
    const dependencies = new Set<ts.SourceFile>();
    let level = [implementationFile];
    for (let depth = 0; depth < 2; depth++) {
      const next = level.flatMap(imports).filter((file) => file !== implementationFile);
      for (const file of next) dependencies.add(file);
      level = next;
    }
    const tests = program
      .getSourceFiles()
      .filter(
        (file) =>
          fileName(file) &&
          /\.(test|spec)\.[cm]?[jt]sx?$/.test(file.fileName) &&
          imports(file).includes(implementationFile),
      );
    const declarationFile = candidate.call.getSourceFile();
    elements[kind + ":" + definition.name] = {
      definition: {
        file: fileName(declarationFile)!,
        line: declarationFile.getLineAndCharacterOfPosition(candidate.call.getStart()).line + 1,
      },
      implementation: ref,
      dependencies: [...dependencies].map((file) => fileName(file)!).sort(),
      tests: tests.map((file) => fileName(file)!).sort(),
    };
  }
  return {
    refs,
    index: {
      repo: { root: ".", commit },
      elements,
      conventions: {
        registry: fileName(entry)!,
        testCommand: "pnpm test",
        manifestCommand: "cms manifest build",
      },
    },
  };
}
